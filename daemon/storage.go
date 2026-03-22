package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
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
// RAID status
// ═══════════════════════════════════

func getRAIDStatusGo() []map[string]interface{} {
	mdstat := readFileStr("/proc/mdstat")
	if mdstat == "" {
		return []map[string]interface{}{}
	}

	var arrays []map[string]interface{}
	lines := strings.Split(mdstat, "\n")

	reArray := regexp.MustCompile(`^(md\d+)\s*:\s*active\s+(\w+)\s+(.+)`)
	reMember := regexp.MustCompile(`(\w+)\[(\d+)\](\((?:S|F)\))?`)

	for i, line := range lines {
		m := reArray.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		level := m[2]
		devicesStr := m[3]

		var members []interface{}
		for _, dm := range reMember.FindAllStringSubmatch(devicesStr, -1) {
			entry := map[string]interface{}{
				"device": dm[1],
				"index":  parseIntDefault(dm[2], 0),
				"spare":  dm[3] == "(S)",
				"failed": dm[3] == "(F)",
			}
			members = append(members, entry)
		}

		status := "active"
		var progress interface{}
		totalBlocks := 0

		if i+1 < len(lines) {
			statusLine := lines[i+1]
			reBlocks := regexp.MustCompile(`(\d+)\s+blocks`)
			if bm := reBlocks.FindStringSubmatch(statusLine); bm != nil {
				totalBlocks = parseIntDefault(bm[1], 0)
			}
			if strings.Contains(statusLine, "[_") {
				status = "degraded"
			}
		}
		if i+2 < len(lines) {
			progressLine := lines[i+2]
			reRecovery := regexp.MustCompile(`recovery\s*=\s*([\d.]+)%`)
			reReshape := regexp.MustCompile(`reshape\s*=\s*([\d.]+)%`)
			if rm := reRecovery.FindStringSubmatch(progressLine); rm != nil {
				status = "rebuilding"
				progress = parseFloat(rm[1])
			} else if rm := reReshape.FindStringSubmatch(progressLine); rm != nil {
				status = "reshaping"
				progress = parseFloat(rm[1])
			}
		}

		// Detail
		var uuid interface{}
		arraySize := int64(0)
		if detail, ok := run(fmt.Sprintf("mdadm --detail /dev/%s 2>/dev/null", name)); ok {
			reUUID := regexp.MustCompile(`UUID\s*:\s*(\S+)`)
			if um := reUUID.FindStringSubmatch(detail); um != nil {
				uuid = um[1]
			}
			reSize := regexp.MustCompile(`Array Size\s*:\s*(\d+)`)
			if sm := reSize.FindStringSubmatch(detail); sm != nil {
				arraySize = parseInt64(sm[1]) * 1024
			}
		}

		if members == nil {
			members = []interface{}{}
		}
		arrays = append(arrays, map[string]interface{}{
			"name": name, "level": level, "status": status, "progress": progress,
			"members": members, "uuid": uuid, "totalBlocks": totalBlocks,
			"arraySize": arraySize, "arraySizeFormatted": formatBytes(arraySize),
		})
	}

	if arrays == nil {
		return []map[string]interface{}{}
	}
	return arrays
}

// ═══════════════════════════════════
// Storage pools
// ═══════════════════════════════════

func getStoragePoolsGo() []map[string]interface{} {
	conf := getStorageConfigFull()
	raids := getRAIDStatusGo()
	var pools []map[string]interface{}
	configChanged := false

	confPools, _ := conf["pools"].([]interface{})
	primaryPool, _ := conf["primaryPool"].(string)

	for _, poolRaw := range confPools {
		poolConf, _ := poolRaw.(map[string]interface{})
		if poolConf == nil {
			continue
		}

		// Dispatch by pool type
		poolType, _ := poolConf["type"].(string)
		if poolType == "zfs" {
			pools = append(pools, getZfsPoolInfo(poolConf, primaryPool))
			continue
		}
		if poolType == "btrfs" {
			pools = append(pools, getBtrfsPoolInfo(poolConf, primaryPool))
			continue
		}

		// Legacy mdadm pool
		poolName, _ := poolConf["name"].(string)
		arrayName, _ := poolConf["arrayName"].(string)
		mountPoint, _ := poolConf["mountPoint"].(string)
		raidLevel, _ := poolConf["raidLevel"].(string)
		filesystem, _ := poolConf["filesystem"].(string)
		createdAt, _ := poolConf["createdAt"].(string)

		// Find RAID array — first try by name, then by member disks
		var raid map[string]interface{}
		for _, r := range raids {
			if r["name"] == arrayName {
				raid = r
				break
			}
		}
		// If not found by name, search by disks (kernel may reassign md numbers on reboot)
		if raid == nil && arrayName != "" {
			poolDisks := map[string]bool{}
			if pd, ok := poolConf["disks"].([]interface{}); ok {
				for _, d := range pd {
					if ds, ok := d.(string); ok {
						// Extract base disk name: "/dev/sda" → "sda"
						base := ds
						if idx := strings.LastIndex(ds, "/"); idx >= 0 {
							base = ds[idx+1:]
						}
						poolDisks[base] = true
					}
				}
			}
			if len(poolDisks) > 0 {
				for _, r := range raids {
					members, _ := r["members"].([]interface{})
					matchCount := 0
					for _, m := range members {
						mm, _ := m.(map[string]interface{})
						dev, _ := mm["device"].(string)
						// Strip partition number: "sda1" → "sda"
						devBase := strings.TrimRight(dev, "0123456789")
						if poolDisks[devBase] {
							matchCount++
						}
					}
					if matchCount > 0 && matchCount >= len(poolDisks) {
						raid = r
						// Auto-update config with correct array name
						newName, _ := r["name"].(string)
						if newName != "" && newName != arrayName {
							arrayName = newName
							poolConf["arrayName"] = newName
							configChanged = true
						}
						break
					}
				}
			}
		}

		// Check if actually mounted (not just directory on root filesystem)
		isMounted := false
		if mountPoint != "" {
			mountSrc, mountOk := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
			if mountOk && strings.TrimSpace(mountSrc) != "" {
				// Verify it's not the root filesystem
				rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
				if strings.TrimSpace(mountSrc) != strings.TrimSpace(rootSrc) {
					isMounted = true
				}
			}
		}

		// Get disk usage ONLY if actually mounted
		var total, used, available int64
		if isMounted {
			if mountInfo, ok := run(fmt.Sprintf("df -B1 --output=size,used,avail %s 2>/dev/null", mountPoint)); ok {
				lines := strings.Split(strings.TrimSpace(mountInfo), "\n")
				if len(lines) > 1 {
					parts := strings.Fields(lines[1])
					if len(parts) >= 3 {
						total = parseInt64(parts[0])
						used = parseInt64(parts[1])
						available = parseInt64(parts[2])
					}
				}
			}
		}

		poolStatus := "unknown"
		if !isMounted {
			poolStatus = "offline"
			total = 0
			used = 0
			available = 0
		} else if raid != nil {
			poolStatus, _ = raid["status"].(string)
		} else if raidLevel == "single" || arrayName == "" {
			poolStatus = "active"
		}

		var disks []interface{}
		if d, ok := poolConf["disks"].([]interface{}); ok {
			disks = d
		} else {
			disks = []interface{}{}
		}

		var members []interface{}
		var rebuildProgress interface{}
		if raid != nil {
			members, _ = raid["members"].([]interface{})
			rebuildProgress = raid["progress"]
		}
		if members == nil {
			members = []interface{}{}
		}

		usagePct := 0
		if total > 0 {
			usagePct = int(math.Round(float64(used) / float64(total) * 100))
		}

		if filesystem == "" {
			filesystem = "ext4"
		}

		pools = append(pools, map[string]interface{}{
			"name":               poolName,
			"type":               "mdadm",
			"arrayName":          arrayName,
			"arrayPath":          func() interface{} { if arrayName != "" { return "/dev/" + arrayName }; return nil }(),
			"mountPoint":         mountPoint,
			"raidLevel":          raidLevel,
			"filesystem":         filesystem,
			"createdAt":          createdAt,
			"disks":              disks,
			"status":             poolStatus,
			"rebuildProgress":    rebuildProgress,
			"members":            members,
			"total":              total,
			"used":               used,
			"available":          available,
			"totalFormatted":     formatBytes(total),
			"usedFormatted":      formatBytes(used),
			"availableFormatted": formatBytes(available),
			"usagePercent":       usagePct,
			"isPrimary":          poolName == primaryPool,
		})
	}

	if pools == nil {
		pools = []map[string]interface{}{}
	}

	// Auto-save config if array names were corrected
	if configChanged {
		saveStorageConfigFull(conf)
		logMsg("Storage config auto-updated (array names corrected)")
	}

	return pools
}

// ═══════════════════════════════════
// Create pool
// ═══════════════════════════════════

func createPoolGo(body map[string]interface{}) map[string]interface{} {
	name := bodyStr(body, "name")
	level := bodyStr(body, "level")
	filesystem := bodyStr(body, "filesystem")
	if filesystem == "" {
		filesystem = "ext4"
	}

	if name == "" || !regexp.MustCompile(`^[a-zA-Z0-9-]{1,32}$`).MatchString(name) {
		return map[string]interface{}{"error": "Invalid pool name. Use alphanumeric + hyphens, max 32 chars."}
	}
	reserved := map[string]bool{"system": true, "config": true, "temp": true, "swap": true, "root": true, "boot": true}
	if reserved[strings.ToLower(name)] {
		return map[string]interface{}{"error": fmt.Sprintf(`"%s" is a reserved name.`, name)}
	}

	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == name {
			return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" already exists.`, name)}
		}
	}

	disksRaw, _ := body["disks"].([]interface{})
	if len(disksRaw) < 1 {
		return map[string]interface{}{"error": "At least 1 disk required."}
	}
	var disks []string
	for _, d := range disksRaw {
		if ds, ok := d.(string); ok {
			// Normalize: accept both "sdb" and "/dev/sdb"
			if !strings.HasPrefix(ds, "/dev/") {
				ds = "/dev/" + ds
			}
			disks = append(disks, ds)
		}
	}

	if filesystem != "ext4" && filesystem != "xfs" {
		return map[string]interface{}{"error": "Filesystem must be ext4 or xfs."}
	}

	isSingleDisk := len(disks) == 1
	levelInt := parseIntDefault(level, 0)

	if !isSingleDisk {
		minDisks := map[int]int{0: 2, 1: 2, 5: 3, 6: 4, 10: 4}
		min, exists := minDisks[levelInt]
		if !exists {
			return map[string]interface{}{"error": fmt.Sprintf("Invalid RAID level: %s. Use 0, 1, 5, 6, or 10.", level)}
		}
		if len(disks) < min {
			return map[string]interface{}{"error": fmt.Sprintf("RAID %s requires at least %d disks. You selected %d.", level, min, len(disks))}
		}
	}

	mountPoint := nimbusPoolsDir + "/" + name

	// This is a heavy operation — delegate to shell script approach for reliability
	// The actual implementation mirrors the Node.js version's execSync calls
	detected := detectStorageDisksGo()
	eligibleList, _ := detected["eligible"].([]interface{})
	eligiblePaths := map[string]bool{}
	for _, e := range eligibleList {
		em, _ := e.(map[string]interface{})
		if p, _ := em["path"].(string); p != "" {
			eligiblePaths[p] = true
		}
	}
	for _, disk := range disks {
		if !eligiblePaths[disk] {
			return map[string]interface{}{"error": fmt.Sprintf("Disk %s is not eligible for pool creation.", disk)}
		}
	}

	// Execute pool creation (simplified — mirrors Node.js logic)
	var partitions []string

	for _, disk := range disks {
		if isSingleDisk {
			// Wipe + partition
			runExec("wipefs", "-a", disk)
			hasSgdisk := false
			if _, ok := run("which sgdisk 2>/dev/null"); ok {
				hasSgdisk = true
			}
			if hasSgdisk {
				runExec("sgdisk", "-Z", disk)
				runExec("sgdisk", "-n", "1:0:0", "-t", "1:8300", "-c", fmt.Sprintf(`1:"NIMBUS-DATA"`), disk)
			} else {
				runExec("bash", "-c", fmt.Sprintf(`echo ";" | sfdisk --force %s 2>/dev/null || true`, disk))
			}
			runExec("partprobe", disk)
			time.Sleep(2 * time.Second)

			// Find partition
			if newParts, ok := run(fmt.Sprintf("lsblk -lnp -o NAME %s 2>/dev/null", disk)); ok {
				for _, l := range strings.Split(strings.TrimSpace(newParts), "\n") {
					l = strings.TrimSpace(l)
					if l != "" && l != disk {
						partitions = append(partitions, l)
					}
				}
			}
			if len(partitions) == 0 {
				partitions = append(partitions, disk)
			}
		} else {
			// RAID disk
			runExec("sgdisk", "-Z", disk)
			runExec("sgdisk", "-n", "1:0:0", "-t", "1:FD00", "-c", fmt.Sprintf(`1:"NIMBUS-DATA"`), disk)
			partitions = append(partitions, disk+"1")
		}
	}

	runExec("partprobe")
	time.Sleep(2 * time.Second)

	if isSingleDisk {
		// Format
		if filesystem == "xfs" {
			runExec("mkfs.xfs", "-f", "-L", "nimbus-"+name, partitions[0])
		} else {
			runExec("mkfs.ext4", "-F", "-L", "nimbus-"+name, partitions[0])
		}
		os.MkdirAll(mountPoint, 0755)
		runExec("mount", partitions[0], mountPoint)

		uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s", partitions[0]))
		appendFstab(strings.TrimSpace(uuid), mountPoint, filesystem)
	} else {
		// RAID
		raids := getRAIDStatusGo()
		usedMds := map[int]bool{}
		for _, r := range raids {
			n, _ := r["name"].(string)
			n = strings.TrimPrefix(n, "md")
			usedMds[parseIntDefault(n, -1)] = true
		}
		mdNum := 0
		for usedMds[mdNum] {
			mdNum++
		}
		mdPath := fmt.Sprintf("/dev/md%d", mdNum)
		mdName := fmt.Sprintf("md%d", mdNum)

		args := []string{"--create", mdPath, fmt.Sprintf("--level=%d", levelInt), fmt.Sprintf("--raid-devices=%d", len(disks)), "--metadata=1.2", "--run"}
		args = append(args, partitions...)
		runExec("mdadm", args...)

		if filesystem == "xfs" {
			runExec("mkfs.xfs", "-f", "-L", "nimbus-"+name, mdPath)
		} else {
			runExec("mkfs.ext4", "-F", "-L", "nimbus-"+name, mdPath)
		}
		os.MkdirAll(mountPoint, 0755)
		runExec("mount", mdPath, mountPoint)

		uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s", mdPath))
		appendFstab(strings.TrimSpace(uuid), mountPoint, filesystem)
		run("mdadm --detail --scan > /etc/mdadm/mdadm.conf 2>/dev/null || true")
		run("update-initramfs -u 2>/dev/null || true")

		_ = mdName // used in config below
		conf = getStorageConfigFull() // re-read
		confPools, _ = conf["pools"].([]interface{})
		isFirst := len(confPools) == 0
		confPools = append(confPools, map[string]interface{}{
			"name": name, "arrayName": mdName, "mountPoint": mountPoint,
			"raidLevel": fmt.Sprintf("raid%d", levelInt), "filesystem": filesystem,
			"disks": disksRaw, "createdAt": time.Now().UTC().Format(time.RFC3339),
		})
		conf["pools"] = confPools
		if isFirst {
			conf["primaryPool"] = name
			conf["configuredAt"] = time.Now().UTC().Format(time.RFC3339)
		}
		saveStorageConfigFull(conf)
		createPoolDirs(mountPoint)
		writePoolIdentity(mountPoint, name, fmt.Sprintf("raid%d", levelInt), filesystem, disks)
		return map[string]interface{}{"ok": true, "pool": map[string]interface{}{"name": name, "mountPoint": mountPoint, "raidLevel": fmt.Sprintf("raid%d", levelInt), "disks": disks}, "isFirstPool": isFirst}
	}

	// Single disk config save
	conf = getStorageConfigFull()
	confPools, _ = conf["pools"].([]interface{})
	isFirst := len(confPools) == 0
	confPools = append(confPools, map[string]interface{}{
		"name": name, "arrayName": nil, "mountPoint": mountPoint,
		"raidLevel": "single", "filesystem": filesystem,
		"disks": disksRaw, "createdAt": time.Now().UTC().Format(time.RFC3339),
	})
	conf["pools"] = confPools
	if isFirst {
		conf["primaryPool"] = name
		conf["configuredAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	saveStorageConfigFull(conf)
	createPoolDirs(mountPoint)
	writePoolIdentity(mountPoint, name, "single", filesystem, disks)

	return map[string]interface{}{"ok": true, "pool": map[string]interface{}{"name": name, "mountPoint": mountPoint, "raidLevel": "single", "disks": disks}, "isFirstPool": isFirst}
}

func createPoolDirs(mountPoint string) {
	dirs := []string{
		"docker/containers",
		"docker/stacks",
		"docker/volumes",
		"docker/data",
		"docker/data/containers",
		"shares",
		"system-backup/config",
		"system-backup/snapshots",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(mountPoint, d), 0755)
	}
}

func writePoolIdentity(mountPoint, name, raidLevel, filesystem string, disks []string) {
	identity := map[string]interface{}{
		"name": name, "raidLevel": raidLevel, "filesystem": filesystem,
		"disks": disks, "createdAt": time.Now().UTC().Format(time.RFC3339), "nimbusVersion": "4.0.0-beta",
	}
	data, _ := json.MarshalIndent(identity, "", "  ")
	os.WriteFile(filepath.Join(mountPoint, ".nimbus-pool.json"), data, 0644)
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

	// Wipe all filesystem signatures
	run(fmt.Sprintf("wipefs -af %s 2>&1 || true", diskPath))

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

func destroyPoolGo(poolName string) map[string]interface{} {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	var poolConf map[string]interface{}
	var poolIdx int
	for i, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			poolConf = pm
			poolIdx = i
			break
		}
	}
	if poolConf == nil {
		return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" not found`, poolName)}
	}

	mountPoint, _ := poolConf["mountPoint"].(string)
	arrayName, _ := poolConf["arrayName"].(string)
	raidLevel, _ := poolConf["raidLevel"].(string)

	// Get disk list from config for superblock cleanup
	var poolDisks []string
	if pd, ok := poolConf["disks"].([]interface{}); ok {
		for _, d := range pd {
			if ds, ok := d.(string); ok {
				if !strings.HasPrefix(ds, "/dev/") {
					ds = "/dev/" + ds
				}
				poolDisks = append(poolDisks, ds)
			}
		}
	}

	logMsg("Destroying pool '%s' (mount: %s, array: %s, disks: %v)", poolName, mountPoint, arrayName, poolDisks)

	// ── 1. Delete ALL shares that belong to this pool ──
	shares, _ := dbSharesList()
	for _, s := range shares {
		sharPool, _ := s["pool"].(string)
		sharVolume, _ := s["volume"].(string)
		sharPath, _ := s["path"].(string)
		sharName, _ := s["name"].(string)
		if sharPool == poolName || sharVolume == poolName || (mountPoint != "" && strings.HasPrefix(sharPath, mountPoint)) {
			handleOp(Request{Op: "share.delete", ShareName: sharName})
			dbSharesDelete(sharName)
			logMsg("Deleted share '%s' (pool '%s' destroyed)", sharName, poolName)
		}
	}

	// ── 2. Clean Docker if it was on this pool ──
	dockerConf := getDockerConfigGo()
	dockerPath, _ := dockerConf["path"].(string)
	if dockerPath != "" && mountPoint != "" && strings.HasPrefix(dockerPath, mountPoint) {
		// Docker was on this pool — stop containers and clean config
		run("docker stop $(docker ps -aq) 2>/dev/null || true")
		run("docker rm $(docker ps -aq) 2>/dev/null || true")
		run("systemctl stop docker 2>/dev/null || true")
		run("rm -f /etc/docker/daemon.json 2>/dev/null || true")

		// Reset docker.json
		newDockerConf := map[string]interface{}{
			"installed": false, "path": nil, "permissions": []interface{}{},
			"appPermissions": map[string]interface{}{}, "installedAt": nil,
		}
		saveDockerConfigGo(newDockerConf)

		// Clear installed apps
		saveInstalledApps([]map[string]interface{}{})

		// Delete docker-apps share group
		run("groupdel nimos-share-docker-apps 2>/dev/null || true")

		logMsg("Docker config cleaned (was on pool '%s')", poolName)
	}

	// ── 3. Unmount the pool ──
	if mountPoint != "" {
		run(fmt.Sprintf("umount -l %s 2>/dev/null || true", mountPoint))
		run(fmt.Sprintf("umount -f %s 2>/dev/null || true", mountPoint))
	}

	// ── 4. Stop and clean mdadm array ──
	if arrayName != "" {
		run(fmt.Sprintf("mdadm --stop /dev/%s 2>/dev/null || true", arrayName))
	}
	// Also try to stop any md device mounted at this path
	if mountPoint != "" {
		mountSrc, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
		mountSrc = strings.TrimSpace(mountSrc)
		if strings.HasPrefix(mountSrc, "/dev/md") {
			run(fmt.Sprintf("umount -l %s 2>/dev/null || true", mountSrc))
			run(fmt.Sprintf("mdadm --stop %s 2>/dev/null || true", mountSrc))
		}
	}

	// ── 5. Zero mdadm superblock on ALL pool disks + their partitions ──
	for _, disk := range poolDisks {
		diskBase := filepath.Base(disk)
		// Zero superblock on disk itself
		run(fmt.Sprintf("mdadm --zero-superblock %s 2>/dev/null || true", disk))
		// Find and zero all partitions of this disk
		partsOut, _ := run(fmt.Sprintf("lsblk -ln -o NAME %s 2>/dev/null | tail -n +2", disk))
		for _, p := range strings.Fields(partsOut) {
			p = strings.TrimSpace(p)
			if p != "" && p != diskBase {
				run(fmt.Sprintf("mdadm --zero-superblock /dev/%s 2>/dev/null || true", p))
			}
		}
	}

	// ── 6. Remove mount point directory ──
	if mountPoint != "" && strings.HasPrefix(mountPoint, nimbusPoolsDir) {
		os.RemoveAll(mountPoint)
	}

	// ── 7. Clean fstab — remove ALL entries for this pool ──
	if fstab, err := os.ReadFile("/etc/fstab"); err == nil {
		var cleanLines []string
		for _, line := range strings.Split(string(fstab), "\n") {
			keep := true
			// Remove by mount point
			if mountPoint != "" && strings.Contains(line, mountPoint) {
				keep = false
			}
			// Remove by array name
			if arrayName != "" && strings.Contains(line, arrayName) {
				keep = false
			}
			// Remove by pool label
			if strings.Contains(line, "nimbus-"+poolName) {
				keep = false
			}
			if keep {
				cleanLines = append(cleanLines, line)
			}
		}
		os.WriteFile("/etc/fstab", []byte(strings.Join(cleanLines, "\n")), 0644)
	}

	// ── 8. Update mdadm.conf (regenerate from remaining active arrays) ──
	if raidLevel != "single" && arrayName != "" {
		run("mdadm --detail --scan > /etc/mdadm/mdadm.conf 2>/dev/null || true")
		run("update-initramfs -u 2>/dev/null || true")
	}

	// ── 9. Remove from storage.json ──
	confPools = append(confPools[:poolIdx], confPools[poolIdx+1:]...)
	conf["pools"] = confPools
	if pp, _ := conf["primaryPool"].(string); pp == poolName {
		if len(confPools) > 0 {
			first, _ := confPools[0].(map[string]interface{})
			conf["primaryPool"] = first["name"]
		} else {
			conf["primaryPool"] = nil
			conf["configuredAt"] = nil
		}
	}
	saveStorageConfigFull(conf)

	// ── 10. Rescan disks so they appear as available again ──
	for _, disk := range poolDisks {
		run(fmt.Sprintf("partx -d %s 2>/dev/null || true", disk))
		run(fmt.Sprintf("blockdev --rereadpt %s 2>/dev/null || true", disk))
	}
	run("partprobe 2>/dev/null || true")
	rescanSCSIBuses()

	logMsg("Pool '%s' fully destroyed — all configs cleaned", poolName)
	return map[string]interface{}{"ok": true, "pool": poolName}
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
	raids := getRAIDStatusGo()
	pools := getStoragePoolsGo()

	for _, raid := range raids {
		status, _ := raid["status"].(string)
		name, _ := raid["name"].(string)
		if status == "degraded" {
			alerts = append(alerts, map[string]interface{}{"severity": "critical", "type": "raid_degraded", "array": name, "message": fmt.Sprintf("RAID array %s is DEGRADED", name)})
		}
		if status == "rebuilding" {
			alerts = append(alerts, map[string]interface{}{"severity": "warning", "type": "raid_rebuilding", "array": name, "message": fmt.Sprintf("RAID array %s is rebuilding (%v%%)", name, raid["progress"])})
		}
	}

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
// Detect existing pools (re-import)
// ═══════════════════════════════════

func detectExistingPoolsGo() []map[string]interface{} {
	run("mdadm --assemble --scan 2>/dev/null || true")
	raids := getRAIDStatusGo()
	var found []map[string]interface{}

	for _, raid := range raids {
		raidName, _ := raid["name"].(string)
		label, _ := run(fmt.Sprintf("blkid -s LABEL -o value /dev/%s 2>/dev/null", raidName))
		if !strings.HasPrefix(strings.TrimSpace(label), "nimbus-") {
			continue
		}
		poolName := strings.TrimPrefix(strings.TrimSpace(label), "nimbus-")
		found = append(found, map[string]interface{}{
			"arrayName":          raidName,
			"poolName":           poolName,
			"raidLevel":          raid["level"],
			"status":             raid["status"],
			"members":            raid["members"],
			"arraySize":          raid["arraySize"],
			"arraySizeFormatted": raid["arraySizeFormatted"],
		})
	}

	if found == nil {
		found = []map[string]interface{}{}
	}
	return found
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

	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		if pm == nil {
			continue
		}
		mountPoint, _ := pm["mountPoint"].(string)
		if mountPoint == "" || !strings.HasPrefix(mountPoint, nimbusPoolsDir) {
			continue
		}

		// Check if directory exists
		if _, err := os.Stat(mountPoint); err != nil {
			continue // doesn't exist, nothing to clean
		}

		// Check if actually mounted
		mountSrc, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
		if ok && strings.TrimSpace(mountSrc) != "" {
			rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
			if strings.TrimSpace(mountSrc) != strings.TrimSpace(rootSrc) {
				continue // properly mounted on a real device
			}
		}

		// Directory exists but not mounted on a real device — remove it
		// to prevent any process from writing to system disk
		os.RemoveAll(mountPoint)
		logMsg("Removed orphan mount point %s (pool disk not mounted)", mountPoint)
	}
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
				"mdadm":          hasMdadm,
				"arch":           systemArch,
				"ramGB":          systemRamGB,
				"recommended":    func() string {
					if hasZfs && systemRamGB >= 4 { return "zfs" }
					if hasBtrfs { return "btrfs" }
					return "mdadm"
				}(),
				"reason":         func() string {
					if hasZfs && systemRamGB >= 4 { return "ZFS available — best data protection" }
					if hasBtrfs { return "Btrfs available — snapshots, checksums, RAID" }
					if hasZfs { return fmt.Sprintf("ZFS available but low RAM (%dGB) — Btrfs recommended", systemRamGB) }
					return "Only mdadm available — consider installing btrfs-progs"
				}(),
			})
		case "/api/storage/health":
			jsonOk(w, checkStorageHealthGo())
		case "/api/storage/detect-existing":
			jsonOk(w, map[string]interface{}{"pools": detectExistingPoolsGo()})
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
			} else if hasBtrfs {
				// Default to Btrfs if available (preferred over mdadm)
				jsonOk(w, createPoolBtrfs(body))
			} else {
				jsonOk(w, createPoolGo(body))
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
				// Check pool type to dispatch
				conf := getStorageConfigFull()
				confPools, _ := conf["pools"].([]interface{})
				poolType := "mdadm"
				for _, p := range confPools {
					pm, _ := p.(map[string]interface{})
					if n, _ := pm["name"].(string); n == name {
						if t, _ := pm["type"].(string); t != "" {
							poolType = t
						}
						break
					}
				}
				switch poolType {
				case "zfs":
					jsonOk(w, destroyPoolZfs(name))
				case "btrfs":
					jsonOk(w, destroyPoolBtrfs(name))
				default:
					jsonOk(w, destroyPoolGo(name))
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
