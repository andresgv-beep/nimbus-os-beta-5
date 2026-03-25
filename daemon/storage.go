package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ═══════════════════════════════════
// Constants
// ═══════════════════════════════════

const nimbusPoolsDir = "/nimbus/pools"

// ═══════════════════════════════════
// Storage config (JSON file)
// ═══════════════════════════════════

func getStorageConfigFull() map[string]interface{} {
	data, err := os.ReadFile(storageConfigFile)
	if err != nil {
		return map[string]interface{}{"pools": []interface{}{}, "primaryPool": nil, "alerts": map[string]interface{}{"email": nil}, "configuredAt": nil}
	}
	var conf map[string]interface{}
	if json.Unmarshal(data, &conf) != nil {
		return map[string]interface{}{"pools": []interface{}{}, "primaryPool": nil}
	}
	return conf
}

func saveStorageConfigFull(config map[string]interface{}) {
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(storageConfigFile, data, 0644)
}

func hasPoolGo() bool {
	conf := getStorageConfigFull()
	pools, _ := conf["pools"].([]interface{})
	return len(pools) > 0
}

// ═══════════════════════════════════
// Disk detection
// ═══════════════════════════════════

func detectStorageDisksGo() map[string]interface{} {
	result := map[string]interface{}{
		"eligible":    []interface{}{},
		"nvme":        []interface{}{},
		"usb":         []interface{}{},
		"provisioned": []interface{}{},
	}

	lsblkRaw, ok := run("lsblk -J -b -o NAME,SIZE,TYPE,ROTA,MOUNTPOINT,MODEL,SERIAL,TRAN,RM,FSTYPE,LABEL,PKNAME 2>/dev/null")
	if !ok || lsblkRaw == "" {
		return result
	}

	var data struct {
		BlockDevices []json.RawMessage `json:"blockdevices"`
	}
	if json.Unmarshal([]byte(lsblkRaw), &data) != nil {
		return result
	}

	// Find root disk
	rootDisk := findRootDiskGo(lsblkRaw)

	var eligible, nvme, usb, provisioned []interface{}
	storageConf := getStorageConfigFull()
	confPools, _ := storageConf["pools"].([]interface{})

	for _, raw := range data.BlockDevices {
		var dev map[string]interface{}
		json.Unmarshal(raw, &dev)

		devType, _ := dev["type"].(string)
		if devType != "disk" {
			continue
		}
		devName, _ := dev["name"].(string)
		if strings.HasPrefix(devName, "loop") || strings.HasPrefix(devName, "ram") || strings.HasPrefix(devName, "zram") {
			continue
		}

		sizeRaw := dev["size"]
		size := jsonToInt64(sizeRaw)
		if size <= 0 {
			continue
		}

		transport, _ := dev["tran"].(string)
		model, _ := dev["model"].(string)
		serial, _ := dev["serial"].(string)
		rotaBool := jsonToBool(dev["rota"])
		removableBool := jsonToBool(dev["rm"])

		diskInfo := map[string]interface{}{
			"name":               devName,
			"path":               "/dev/" + devName,
			"model":              strings.TrimSpace(model),
			"serial":             strings.TrimSpace(serial),
			"size":               size,
			"sizeFormatted":      formatBytes(size),
			"transport":          transport,
			"rotational":         rotaBool,
			"removable":          removableBool,
			"partitions":         []interface{}{},
			"smart":              nil,
			"temperature":        nil,
			"isBoot":             devName == rootDisk,
			"freeSpace":          int64(0),
			"freeSpaceFormatted": "0 B",
		}

		// Parse partitions
		var partitions []interface{}
		var usedSpace int64
		if children, ok := dev["children"].([]interface{}); ok {
			for _, child := range children {
				cm, ok := child.(map[string]interface{})
				if !ok {
					continue
				}
				partSize := jsonToInt64(cm["size"])
				usedSpace += partSize
				partitions = append(partitions, map[string]interface{}{
					"name":          cm["name"],
					"path":          "/dev/" + fmt.Sprintf("%v", cm["name"]),
					"size":          partSize,
					"sizeFormatted": formatBytes(partSize),
					"fstype":        cm["fstype"],
					"label":         cm["label"],
					"mountpoint":    cm["mountpoint"],
				})
			}
		}
		if partitions == nil {
			partitions = []interface{}{}
		}
		diskInfo["partitions"] = partitions

		freeSpace := size - usedSpace
		if freeSpace < 0 {
			freeSpace = 0
		}
		diskInfo["freeSpace"] = freeSpace
		diskInfo["freeSpaceFormatted"] = formatBytes(freeSpace)

		// SMART
		if hasSmartctl {
			if health, ok := run(fmt.Sprintf("smartctl -H /dev/%s 2>/dev/null", devName)); ok && health != "" {
				if strings.Contains(health, "PASSED") {
					diskInfo["smart"] = "PASSED"
				} else if strings.Contains(health, "FAILED") {
					diskInfo["smart"] = "FAILED"
				} else {
					diskInfo["smart"] = "UNKNOWN"
				}
			}
			if temp, ok := run(fmt.Sprintf("smartctl -A /dev/%s 2>/dev/null | grep -i temperature | head -1", devName)); ok && temp != "" {
				re := regexp.MustCompile(`(\d+)\s*$`)
				if m := re.FindStringSubmatch(temp); m != nil {
					diskInfo["temperature"] = parseIntDefault(m[1], 0)
				}
			}
		}

		// Classify
		isUsb := transport == "usb"
		minPoolSize := int64(10 * 1024 * 1024 * 1024)

		if isUsb && (removableBool || size < minPoolSize) {
			diskInfo["classification"] = "usb"
			usb = append(usb, diskInfo)
			continue
		}

		if strings.HasPrefix(devName, "nvme") {
			if devName == rootDisk {
				diskInfo["classification"] = "nvme-system"
			} else {
				diskInfo["classification"] = "nvme-cache"
			}
			nvme = append(nvme, diskInfo)
			continue
		}

		// Check if already in pool
		hasNimbusLabel := false
		for _, p := range partitions {
			pm, _ := p.(map[string]interface{})
			label, _ := pm["label"].(string)
			if strings.HasPrefix(label, "NIMBUS-") {
				hasNimbusLabel = true
				break
			}
		}
		inPool := false
		poolName := ""
		for _, pool := range confPools {
			pm, _ := pool.(map[string]interface{})
			poolDisks, _ := pm["disks"].([]interface{})
			for _, d := range poolDisks {
				if ds, _ := d.(string); ds == "/dev/"+devName {
					inPool = true
					poolName, _ = pm["name"].(string)
					break
				}
			}
		}

		if hasNimbusLabel || inPool {
			diskInfo["classification"] = "provisioned"
			diskInfo["poolName"] = poolName
			provisioned = append(provisioned, diskInfo)
			continue
		}

		// Eligible
		isBoot := devName == rootDisk
		if isBoot {
			// System disk — NEVER eligible for pool creation or wipe
			continue
		} else {
			if rotaBool {
				diskInfo["classification"] = "hdd"
			} else {
				diskInfo["classification"] = "ssd"
			}
			diskInfo["availableSpace"] = size
			diskInfo["availableSpaceFormatted"] = formatBytes(size)
			diskInfo["hasExistingData"] = len(partitions) > 0
			eligible = append(eligible, diskInfo)
		}
	}

	if eligible == nil {
		eligible = []interface{}{}
	}
	if nvme == nil {
		nvme = []interface{}{}
	}
	if usb == nil {
		usb = []interface{}{}
	}
	if provisioned == nil {
		provisioned = []interface{}{}
	}
	result["eligible"] = eligible
	result["nvme"] = nvme
	result["usb"] = usb
	result["provisioned"] = provisioned
	return result
}

func findRootDiskGo(lsblkJSON string) string {
	var data struct {
		BlockDevices []struct {
			Name     string `json:"name"`
			Children []struct {
				Mountpoint interface{} `json:"mountpoint"`
			} `json:"children"`
			Mountpoint interface{} `json:"mountpoint"`
		} `json:"blockdevices"`
	}
	json.Unmarshal([]byte(lsblkJSON), &data)
	for _, dev := range data.BlockDevices {
		for _, child := range dev.Children {
			if mp, _ := child.Mountpoint.(string); mp == "/" {
				return dev.Name
			}
		}
		if mp, _ := dev.Mountpoint.(string); mp == "/" {
			return dev.Name
		}
	}
	return ""
}

// ═══════════════════════════════════
// Storage pools
// ═══════════════════════════════════

func getStoragePoolsGo() []map[string]interface{} {
	conf := getStorageConfigFull()
	var pools []map[string]interface{}

	confPools, _ := conf["pools"].([]interface{})
	primaryPool, _ := conf["primaryPool"].(string)

	for _, poolRaw := range confPools {
		poolConf, _ := poolRaw.(map[string]interface{})
		if poolConf == nil {
			continue
		}

		poolType, _ := poolConf["type"].(string)
		switch poolType {
		case "zfs":
			pools = append(pools, getZfsPoolInfo(poolConf, primaryPool))
		case "btrfs":
			pools = append(pools, getBtrfsPoolInfo(poolConf, primaryPool))
		default:
			logMsg("getStoragePoolsGo: skipping unsupported pool type '%s'", poolType)
		}
	}

	if pools == nil {
		pools = []map[string]interface{}{}
	}
	return pools
}

func createPoolDirs(mountPoint string) {
	dirs := []string{
		"shares",
		"system-backup/config",
		"system-backup/snapshots",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(mountPoint, d), 0755)
	}
}

func appendFstab(uuid, mountPoint, filesystem string) {
	// Check if already in fstab
	existing, _ := os.ReadFile("/etc/fstab")
	if strings.Contains(string(existing), mountPoint) {
		return
	}

	var entry string
	if uuid != "" {
		entry = fmt.Sprintf("UUID=%s %s %s defaults,nofail,noatime 0 2\n", uuid, mountPoint, filesystem)
	} else {
		// Fallback: find device by mount point
		out, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
		device := strings.TrimSpace(out)
		if device == "" {
			// Try md device directly
			out2, _ := run(fmt.Sprintf("df %s 2>/dev/null | tail -1 | awk '{print $1}'", mountPoint))
			device = strings.TrimSpace(out2)
		}
		if device != "" {
			entry = fmt.Sprintf("%s %s %s defaults,nofail,noatime 0 2\n", device, mountPoint, filesystem)
		} else {
			log.Printf("appendFstab: cannot determine device for %s, skipping", mountPoint)
			return
		}
	}

	f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("appendFstab: cannot open /etc/fstab: %v", err)
		return
	}
	defer f.Close()
	f.WriteString(entry)
	log.Printf("appendFstab: added %s to fstab", mountPoint)
}

func runExec(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Run()
}

// ═══════════════════════════════════
// Wipe, Destroy, Backup, Health
// ═══════════════════════════════════

func wipeDiskGo(diskPath string) map[string]interface{} {
	detected := detectStorageDisksGo()
	allDisks := append(detected["eligible"].([]interface{}), detected["provisioned"].([]interface{})...)
	var diskInfo map[string]interface{}
	for _, d := range allDisks {
		dm, _ := d.(map[string]interface{})
		if p, _ := dm["path"].(string); p == diskPath {
			diskInfo = dm
			break
		}
	}
	if diskInfo == nil {
		return map[string]interface{}{"error": fmt.Sprintf("Disk %s not found or not wipeable", diskPath)}
	}
	if isBoot, _ := diskInfo["isBoot"].(bool); isBoot {
		return map[string]interface{}{"error": "Cannot wipe the boot disk"}
	}

	diskBase := filepath.Base(diskPath)

	// ── Phase 1: Stop everything using this disk ──

	// Find any Btrfs filesystem using this disk and unmount it
	btrfsShow, _ := run(fmt.Sprintf("btrfs filesystem show %s 2>/dev/null", diskPath))
	if btrfsShow != "" && !strings.Contains(btrfsShow, "No valid") {
		// Find mount point for any Btrfs fs containing this disk
		for _, line := range strings.Split(btrfsShow, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "devid") && strings.Contains(line, "path") {
				// Extract device path from line like: devid    1 size 1.82TiB used 2.01GiB path /dev/sda
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "path" && i+1 < len(parts) {
						dev := parts[i+1]
						mnt, _ := run(fmt.Sprintf("findmnt -n -o TARGET %s 2>/dev/null", dev))
						mnt = strings.TrimSpace(mnt)
						if mnt != "" {
							run(fmt.Sprintf("umount -l %s 2>/dev/null || true", mnt))
						}
					}
				}
			}
		}
	}

	// ZFS: check if disk is part of a zpool
	if hasZfs {
		zpoolStatus, _ := run("zpool status 2>/dev/null")
		if strings.Contains(zpoolStatus, filepath.Base(diskPath)) {
			// Find which pool and export/destroy it if needed
			// For now just try to export all pools using this disk
			run(fmt.Sprintf("zpool labelclear -f %s 2>/dev/null || true", diskPath))
		}
	}

	// Unmount all partitions
	partitions, _ := run(fmt.Sprintf("lsblk -ln -o NAME %s 2>/dev/null | tail -n +2", diskPath))
	for _, p := range strings.Fields(partitions) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		run(fmt.Sprintf("swapoff /dev/%s 2>/dev/null || true", p))
		run(fmt.Sprintf("umount -l /dev/%s 2>/dev/null || true", p))   // lazy unmount
		run(fmt.Sprintf("umount -f /dev/%s 2>/dev/null || true", p))   // force unmount
	}
	run(fmt.Sprintf("umount -l %s 2>/dev/null || true", diskPath))
	run(fmt.Sprintf("umount -f %s 2>/dev/null || true", diskPath))

	// Stop any mdadm arrays using this disk
	if mdstat, err := os.ReadFile("/proc/mdstat"); err == nil {
		lines := strings.Split(string(mdstat), "\n")
		for _, line := range lines {
			if strings.Contains(line, diskBase) {
				parts := strings.Fields(line)
				if len(parts) > 0 && strings.HasPrefix(parts[0], "md") {
					mdDev := "/dev/" + parts[0]
					run(fmt.Sprintf("umount -l %s 2>/dev/null || true", mdDev))
					run(fmt.Sprintf("mdadm --stop %s 2>/dev/null || true", mdDev))
				}
			}
		}
	}

	// Remove any device-mapper mappings on this disk (LVM, LUKS, etc.)
	partitions2, _ := run(fmt.Sprintf("lsblk -ln -o NAME %s 2>/dev/null | tail -n +2", diskPath))
	for _, p := range strings.Fields(partitions2) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Remove dm mappings
		run(fmt.Sprintf("dmsetup remove /dev/%s 2>/dev/null || true", p))
		// Close LUKS if open
		run(fmt.Sprintf("cryptsetup close /dev/%s 2>/dev/null || true", p))
		// Zero mdadm superblock
		run(fmt.Sprintf("mdadm --zero-superblock /dev/%s 2>/dev/null || true", p))
	}
	run(fmt.Sprintf("mdadm --zero-superblock %s 2>/dev/null || true", diskPath))

	// ── Phase 2: Destroy partition table and signatures ──

	// First: wipefs on all partitions (clears Btrfs/ZFS/ext4 signatures)
	partsForWipe, _ := run(fmt.Sprintf("lsblk -ln -o NAME %s 2>/dev/null | tail -n +2", diskPath))
	for _, p := range strings.Fields(partsForWipe) {
		p = strings.TrimSpace(p)
		if p != "" {
			run(fmt.Sprintf("wipefs -af /dev/%s 2>/dev/null || true", p))
		}
	}
	// Then wipefs on the disk itself
	run(fmt.Sprintf("wipefs -af %s 2>/dev/null || true", diskPath))

	// Clear ZFS labels if ZFS is available
	if hasZfs {
		run(fmt.Sprintf("zpool labelclear -f %s 2>/dev/null || true", diskPath))
	}

	// Zero first and last 10MB (GPT has backup at end of disk)
	run(fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 conv=notrunc 2>/dev/null || true", diskPath))
	diskSize, _ := run(fmt.Sprintf("blockdev --getsize64 %s 2>/dev/null", diskPath))
	if size := strings.TrimSpace(diskSize); size != "" {
		if sizeInt, err := strconv.ParseInt(size, 10, 64); err == nil && sizeInt > 20*1024*1024 {
			seekMB := (sizeInt / (1024 * 1024)) - 10
			run(fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 seek=%d conv=notrunc 2>/dev/null || true", diskPath, seekMB))
		}
	}

	// Destroy GPT with sgdisk
	if _, ok := run("which sgdisk 2>/dev/null"); ok {
		run(fmt.Sprintf("sgdisk -Z %s 2>/dev/null || true", diskPath))
	}

	// Final wipefs pass (catch anything sgdisk/dd missed)
	run(fmt.Sprintf("wipefs -af %s 2>/dev/null || true", diskPath))

	// ── Phase 3: Force kernel to forget partitions ──

	// Method 1: partx --delete (most reliable for removing stale partition entries)
	run(fmt.Sprintf("partx -d %s 2>/dev/null || true", diskPath))

	// Method 2: Delete each partition individually via partx (safer than sysfs)
	for i := 1; i <= 128; i++ {
		partPath := fmt.Sprintf("/sys/block/%s/%s%d", diskBase, diskBase, i)
		if _, err := os.Stat(partPath); err == nil {
			run(fmt.Sprintf("partx -d --nr %d %s 2>/dev/null || true", i, diskPath))
		} else {
			break
		}
	}

	// Method 3: blockdev --rereadpt (tell kernel to re-read — should see empty table)
	run(fmt.Sprintf("blockdev --rereadpt %s 2>/dev/null || true", diskPath))

	// Method 4: partprobe
	run(fmt.Sprintf("partprobe %s 2>/dev/null || true", diskPath))

	// Wait for udev to settle
	run("udevadm settle --timeout=5 2>/dev/null || true")
	time.Sleep(2 * time.Second)

	// ── Phase 4: Rescan to make sure disk is still visible ──
	// The wipe may cause the kernel to lose track of the disk temporarily
	// Rescan SCSI/SATA buses to ensure the disk device is alive
	rescanSCSIBuses()

	// ── Phase 5: Verify ──

	// Re-read one more time
	run(fmt.Sprintf("blockdev --rereadpt %s 2>/dev/null || true", diskPath))
	run(fmt.Sprintf("partprobe %s 2>/dev/null || true", diskPath))
	run("udevadm settle --timeout=3 2>/dev/null || true")
	time.Sleep(1 * time.Second)

	verifyParts, _ := run(fmt.Sprintf("lsblk -ln -o NAME %s 2>/dev/null | tail -n +2", diskPath))
	remainingParts := strings.TrimSpace(verifyParts)

	if remainingParts != "" {
		// Last resort: check if the partitions are truly gone from the partition table
		// even if the kernel still shows them (stale device nodes)
		tableCheck, _ := run(fmt.Sprintf("sfdisk -d %s 2>/dev/null", diskPath))
		if strings.TrimSpace(tableCheck) == "" || !strings.Contains(tableCheck, "start=") {
			// Partition table is actually empty — kernel just hasn't caught up
			// This is safe to proceed — the pool creation will re-partition
			log.Printf("Wipe: %s partition table is empty but kernel still shows stale nodes. Safe to proceed.", diskPath)
			return map[string]interface{}{"ok": true, "disk": diskPath, "note": "Partition table cleared. Stale kernel entries will be updated on next use."}
		}

		// Partitions truly still there — report but don't fail hard
		// because pool creation will re-partition anyway
		log.Printf("Wipe: %s still shows partitions: %s — pool creation will force re-partition", diskPath, remainingParts)
		return map[string]interface{}{"ok": true, "disk": diskPath, "warning": "Disk wiped but kernel still shows old partitions. This is normal — they will be overwritten when creating a pool."}
	}

	return map[string]interface{}{"ok": true, "disk": diskPath}
}

// rescanSCSIBuses tells the kernel to rescan all SCSI/SATA host buses
// This re-discovers disks that may have been lost during aggressive wipe operations
func rescanSCSIBuses() {
	// Find all SCSI host adapters and trigger rescan
	entries, err := os.ReadDir("/sys/class/scsi_host")
	if err != nil {
		return
	}
	for _, e := range entries {
		scanPath := filepath.Join("/sys/class/scsi_host", e.Name(), "scan")
		os.WriteFile(scanPath, []byte("- - -"), 0200)
	}
	// Wait for udev to process the new devices
	run("udevadm settle --timeout=5 2>/dev/null || true")
	time.Sleep(1 * time.Second)
}

func backupConfigToPoolGo() {
	conf := getStorageConfigFull()
	primaryPool, _ := conf["primaryPool"].(string)
	if primaryPool == "" {
		return
	}
	confPools, _ := conf["pools"].([]interface{})
	var mountPoint string
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == primaryPool {
			mountPoint, _ = pm["mountPoint"].(string)
			break
		}
	}
	if mountPoint == "" {
		return
	}

	backupDir := filepath.Join(mountPoint, "system-backup", "config")
	snapshotDir := filepath.Join(mountPoint, "system-backup", "snapshots",
		strings.ReplaceAll(strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339)[:19], ":", "-"), ".", "-"))

	os.MkdirAll(backupDir, 0755)
	os.MkdirAll(snapshotDir, 0755)

	files := []string{"users.json", "shares.json", "docker.json", "installed-apps.json", "storage.json"}
	for _, file := range files {
		src := filepath.Join(configDir, file)
		if data, err := os.ReadFile(src); err == nil {
			os.WriteFile(filepath.Join(backupDir, file), data, 0644)
			os.WriteFile(filepath.Join(snapshotDir, file), data, 0644)
		}
	}

	// Keep last 5 snapshots
	snapshotsBase := filepath.Join(mountPoint, "system-backup", "snapshots")
	entries, _ := os.ReadDir(snapshotsBase)
	if len(entries) > 5 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		for i := 5; i < len(names); i++ {
			os.RemoveAll(filepath.Join(snapshotsBase, names[i]))
		}
	}
}

var storageAlertsGo []map[string]interface{}

func checkStorageHealthGo() []map[string]interface{} {
	var alerts []map[string]interface{}
	pools := getStoragePoolsGo()

	for _, pool := range pools {
		pct, _ := pool["usagePercent"].(int)
		name, _ := pool["name"].(string)
		if pct >= 95 {
			alerts = append(alerts, map[string]interface{}{"severity": "critical", "type": "pool_full", "pool": name, "message": fmt.Sprintf(`Pool "%s" is %d%% full`, name, pct)})
		} else if pct >= 85 {
			alerts = append(alerts, map[string]interface{}{"severity": "warning", "type": "pool_warning", "pool": name, "message": fmt.Sprintf(`Pool "%s" is %d%% full`, name, pct)})
		}
	}

	if alerts == nil {
		alerts = []map[string]interface{}{}
	}
	storageAlertsGo = alerts
	return alerts
}

// ═══════════════════════════════════
// Scan for restorable pools
// ═══════════════════════════════════

func scanForRestorablePoolsGo() []map[string]interface{} {
	var found []map[string]interface{}
	conf := getStorageConfigFull()

	blkid, _ := run("blkid 2>/dev/null")
	for _, line := range strings.Split(blkid, "\n") {
		if line == "" {
			continue
		}
		reDevice := regexp.MustCompile(`^(/dev/\S+):`)
		dm := reDevice.FindStringSubmatch(line)
		if dm == nil {
			continue
		}
		dev := dm[1]
		if !strings.Contains(line, `TYPE="ext4"`) && !strings.Contains(line, `TYPE="xfs"`) {
			continue
		}

		// Try to mount and check for .nimbus-pool.json
		tmpMount := fmt.Sprintf("/tmp/nimbus-scan-%s", strings.ReplaceAll(dev, "/", "_"))
		existing, _ := run(fmt.Sprintf("findmnt -n -o TARGET %s 2>/dev/null", dev))
		existing = strings.TrimSpace(existing)

		mountDir := existing
		didMount := false
		if mountDir == "" {
			os.MkdirAll(tmpMount, 0755)
			if _, ok := run(fmt.Sprintf("mount -o ro %s %s 2>/dev/null", dev, tmpMount)); ok {
				mountDir = tmpMount
				didMount = true
			}
		}

		if mountDir != "" {
			identityFile := filepath.Join(mountDir, ".nimbus-pool.json")
			if data, err := os.ReadFile(identityFile); err == nil {
				var identity map[string]interface{}
				json.Unmarshal(data, &identity)

				poolName, _ := identity["name"].(string)
				alreadyConfigured := false
				confPools, _ := conf["pools"].([]interface{})
				for _, p := range confPools {
					pm, _ := p.(map[string]interface{})
					if n, _ := pm["name"].(string); n == poolName {
						alreadyConfigured = true
					}
				}

				found = append(found, map[string]interface{}{
					"device":            dev,
					"pool":              identity,
					"alreadyConfigured": alreadyConfigured,
					"currentMount":      existing,
				})
			}
		}

		if didMount {
			run(fmt.Sprintf("umount %s 2>/dev/null || true", tmpMount))
			os.Remove(tmpMount)
		}
	}

	if found == nil {
		found = []map[string]interface{}{}
	}
	return found
}

// ═══════════════════════════════════
// Restore pool
// ═══════════════════════════════════

func restorePoolGo(device, poolName string) map[string]interface{} {
	if device == "" {
		return map[string]interface{}{"error": "Device path required"}
	}
	if _, err := os.Stat(device); err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("Device %s not found", device)}
	}

	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" already configured`, poolName)}
		}
	}

	mountPoint := nimbusPoolsDir + "/" + poolName
	os.MkdirAll(mountPoint, 0755)
	run(fmt.Sprintf("mount %s %s 2>/dev/null", device, mountPoint))

	fstype, _ := run(fmt.Sprintf("blkid -s TYPE -o value %s 2>/dev/null", device))
	fstype = strings.TrimSpace(fstype)
	if fstype == "" {
		fstype = "ext4"
	}

	uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", device))
	uuid = strings.TrimSpace(uuid)
	if uuid != "" {
		appendFstab(uuid, mountPoint, fstype)
	}

	poolEntry := map[string]interface{}{
		"name": poolName, "arrayName": nil, "mountPoint": mountPoint,
		"raidLevel": "single", "filesystem": fstype,
		"disks": []interface{}{device}, "createdAt": time.Now().UTC().Format(time.RFC3339),
		"restoredAt": time.Now().UTC().Format(time.RFC3339), "imported": true,
	}
	confPools = append(confPools, poolEntry)
	conf["pools"] = confPools
	if conf["primaryPool"] == nil {
		conf["primaryPool"] = poolName
	}
	saveStorageConfigFull(conf)

	return map[string]interface{}{"ok": true, "pool": poolEntry}
}

// ═══════════════════════════════════
// Start health monitoring
// ═══════════════════════════════════

func startStorageMonitoring() {
	// On startup: clean orphan mount point directories
	// If a pool dir exists but nothing is mounted there, remove it
	// to prevent writes going to the system disk
	cleanOrphanMountPoints()

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			checkStorageHealthGo()
			cleanOrphanMountPoints()
		}
	}()
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			if hasPoolGo() {
				backupConfigToPoolGo()
			}
		}
	}()
}

func cleanOrphanMountPoints() {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	// Build set of known pool mount points — NEVER delete these
	knownMounts := map[string]bool{}
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		if pm == nil {
			continue
		}
		if mp, _ := pm["mountPoint"].(string); mp != "" {
			knownMounts[mp] = true
		}
	}

	// Scan /nimbus/pools/ for directories that aren't known pools
	entries, err := os.ReadDir(nimbusPoolsDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(nimbusPoolsDir, e.Name())

		// Skip if this is a known pool — never delete
		if knownMounts[dirPath] {
			continue
		}

		// This directory is not in storage.json — it's orphaned
		// Check if it's mounted on something real
		mountSrc, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", dirPath))
		if ok && strings.TrimSpace(mountSrc) != "" {
			rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
			if strings.TrimSpace(mountSrc) != strings.TrimSpace(rootSrc) {
				continue // mounted on a real device, leave it
			}
		}

		// Orphan directory on system disk — remove it
		os.RemoveAll(dirPath)
		logMsg("Removed orphan mount point %s (not in storage.json, not mounted)", dirPath)
	}
}

// ═══════════════════════════════════
// Storage Startup
// Runs once at daemon start — mounts all pools and creates pool dirs
// Docker is managed independently by its own app
// ═══════════════════════════════════

func startupStorage() {
	logMsg("startup: Beginning storage initialization...")

	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	if len(confPools) == 0 {
		logMsg("startup: No pools configured — nothing to do")
		return
	}

	// ── 1. Verify all pools are mounted ──
	mountedPools := 0
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		if pm == nil {
			continue
		}
		poolName, _ := pm["name"].(string)
		poolType, _ := pm["type"].(string)
		mountPoint, _ := pm["mountPoint"].(string)

		if mountPoint == "" {
			logMsg("startup: Pool '%s' has no mount point — skipping", poolName)
			continue
		}

		// Check if already mounted
		mountSrc, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
		if strings.TrimSpace(mountSrc) != "" {
			rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
			if strings.TrimSpace(mountSrc) != strings.TrimSpace(rootSrc) {
				logMsg("startup: Pool '%s' (%s) already mounted at %s", poolName, poolType, mountPoint)
				mountedPools++
				continue
			}
		}

		// Not mounted — try to mount based on type
		logMsg("startup: Pool '%s' (%s) not mounted — attempting mount...", poolName, poolType)
		os.MkdirAll(mountPoint, 0755)

		mounted := false
		switch poolType {
		case "zfs":
			zpoolName, _ := pm["zpoolName"].(string)
			if zpoolName != "" {
				if out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName)); strings.TrimSpace(out) == "" {
					run(fmt.Sprintf("zpool import -f %s 2>/dev/null || true", zpoolName))
				}
				run(fmt.Sprintf("zfs set mountpoint=%s %s 2>/dev/null", mountPoint, zpoolName))
				run(fmt.Sprintf("zfs mount %s 2>/dev/null || true", zpoolName))
				if out, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint)); strings.TrimSpace(out) != "" {
					mounted = true
				}
			}

		case "btrfs":
			run(fmt.Sprintf("mount %s 2>/dev/null || true", mountPoint))
			if out, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint)); strings.TrimSpace(out) != "" {
				mounted = true
			} else {
				label := "nimbus-" + poolName
				run(fmt.Sprintf("mount -t btrfs -o defaults,noatime,compress=zstd:3 LABEL=%s %s 2>/dev/null || true", label, mountPoint))
				if out2, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint)); strings.TrimSpace(out2) != "" {
					mounted = true
				}
			}

		default:
			run(fmt.Sprintf("mount %s 2>/dev/null || true", mountPoint))
			if out, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint)); strings.TrimSpace(out) != "" {
				mounted = true
			}
		}

		if mounted {
			logMsg("startup: Pool '%s' mounted successfully", poolName)
			mountedPools++
		} else {
			logMsg("startup: WARNING — Pool '%s' could not be mounted!", poolName)
		}
	}

	logMsg("startup: %d/%d pools mounted", mountedPools, len(confPools))

	// ── 2. Create pool directories if missing ──
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		if pm == nil {
			continue
		}
		mountPoint, _ := pm["mountPoint"].(string)
		if mountPoint == "" {
			continue
		}
		mountSrc, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
		rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
		if strings.TrimSpace(mountSrc) == "" || strings.TrimSpace(mountSrc) == strings.TrimSpace(rootSrc) {
			continue
		}
		createPoolDirs(mountPoint)
	}

	logMsg("startup: Storage initialization complete")
}

// ═══════════════════════════════════
// JSON helpers
// ═══════════════════════════════════

func jsonToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case string:
		return parseInt64(val)
	case json.Number:
		n, _ := val.Int64()
		return n
	}
	return 0
}

func jsonToBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "1" || val == "true"
	case float64:
		return val == 1
	}
	return false
}

// ═══════════════════════════════════
// Storage HTTP routes
// ═══════════════════════════════════

func handleStorageRoutes(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	method := r.Method

	// GET routes (need admin — storage is sensitive)
	if method == "GET" {
		session := requireAdmin(w, r)
		if session == nil {
			return
		}

		// Try ZFS routes first
		if hasZfs && handleZfsRoutes(w, r, method, urlPath, session, nil) {
			return
		}
		// Try Btrfs routes
		if hasBtrfs && handleBtrfsRoutes(w, r, method, urlPath, session, nil) {
			return
		}

		switch urlPath {
		case "/api/storage", "/api/storage/pools":
			jsonOk(w, getStoragePoolsGo())
		case "/api/storage/disks":
			jsonOk(w, detectStorageDisksGo())
		case "/api/storage/status":
			jsonOk(w, map[string]interface{}{"pools": getStoragePoolsGo(), "alerts": storageAlertsGo, "hasPool": hasPoolGo()})
		case "/api/storage/alerts":
			jsonOk(w, map[string]interface{}{"alerts": storageAlertsGo})
		case "/api/storage/capabilities":
			jsonOk(w, map[string]interface{}{
				"zfs":            hasZfs,
				"btrfs":          hasBtrfs,
				"arch":           systemArch,
				"ramGB":          systemRamGB,
				"recommended":    func() string {
					if hasZfs && systemRamGB >= 4 { return "zfs" }
					if hasBtrfs { return "btrfs" }
					return "none"
				}(),
				"reason":         func() string {
					if hasZfs && systemRamGB >= 4 { return "ZFS available — best data protection" }
					if hasBtrfs { return "Btrfs available — snapshots, checksums, RAID" }
					if hasZfs { return fmt.Sprintf("ZFS available but low RAM (%dGB) — Btrfs recommended", systemRamGB) }
					return "No supported filesystem found — install zfsutils-linux or btrfs-progs"
				}(),
			})
		case "/api/storage/health":
			jsonOk(w, checkStorageHealthGo())
		case "/api/storage/restorable":
			jsonOk(w, map[string]interface{}{"pools": scanForRestorablePoolsGo()})
		default:
			jsonError(w, 404, "Not found")
		}
		return
	}

	// POST/DELETE routes (need admin)
	if method == "POST" || method == "DELETE" || method == "PUT" {
		session := requireAdmin(w, r)
		if session == nil {
			return
		}

		// Try ZFS routes first
		body, _ := readBody(r)
		if hasZfs && handleZfsRoutes(w, r, method, urlPath, session, body) {
			return
		}
		// Try Btrfs routes
		if hasBtrfs && handleBtrfsRoutes(w, r, method, urlPath, session, body) {
			return
		}

		switch urlPath {
		case "/api/storage/pool":
			poolType := bodyStr(body, "type")
			if poolType == "zfs" && hasZfs {
				jsonOk(w, createPoolZfs(body))
			} else if poolType == "btrfs" && hasBtrfs {
				jsonOk(w, createPoolBtrfs(body))
			} else if hasZfs && systemRamGB >= 4 {
				jsonOk(w, createPoolZfs(body))
			} else if hasBtrfs {
				jsonOk(w, createPoolBtrfs(body))
			} else {
				jsonError(w, 400, "No supported filesystem available (need ZFS or Btrfs)")
			}
		case "/api/storage/scan":
			rescanSCSIBuses()
			jsonOk(w, map[string]interface{}{"ok": true, "disks": detectStorageDisksGo()})
		case "/api/storage/backup":
			backupConfigToPoolGo()
			jsonOk(w, map[string]interface{}{"ok": true})
		case "/api/storage/wipe":
			disk := bodyStr(body, "disk")
			if disk == "" {
				jsonError(w, 400, "Provide disk path")
			} else {
				jsonOk(w, wipeDiskGo(disk))
			}
		case "/api/storage/pool/destroy":
			name := bodyStr(body, "name")
			if name == "" {
				jsonError(w, 400, "Provide pool name")
			} else {
				conf := getStorageConfigFull()
				confPools, _ := conf["pools"].([]interface{})
				poolType := ""
				for _, p := range confPools {
					pm, _ := p.(map[string]interface{})
					if n, _ := pm["name"].(string); n == name {
						poolType, _ = pm["type"].(string)
						break
					}
				}
				switch poolType {
				case "zfs":
					jsonOk(w, destroyPoolZfs(name))
				case "btrfs":
					jsonOk(w, destroyPoolBtrfs(name))
				default:
					jsonError(w, 400, fmt.Sprintf("Unknown pool type '%s'", poolType))
				}
			}
		case "/api/storage/pool/restore":
			device := bodyStr(body, "device")
			name := bodyStr(body, "name")
			if device == "" || name == "" {
				jsonError(w, 400, "Provide device and name")
			} else {
				jsonOk(w, restorePoolGo(device, name))
			}
		default:
			jsonError(w, 404, "Not found")
		}
		return
	}

	jsonError(w, 405, "Method not allowed")
}
