package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ═══════════════════════════════════
// Btrfs Detection
// ═══════════════════════════════════

var hasBtrfs bool

func detectBtrfs() {
	if _, ok := run("which mkfs.btrfs 2>/dev/null"); ok {
		hasBtrfs = true
		logMsg("Btrfs: available")
	} else {
		logMsg("Btrfs: not available (btrfs-progs not installed)")
	}
}

// ═══════════════════════════════════
// Btrfs Pool Create
// ═══════════════════════════════════

func createPoolBtrfs(body map[string]interface{}) map[string]interface{} {
	name := bodyStr(body, "name")
	profile := bodyStr(body, "profile") // single, raid1, raid10
	if profile == "" {
		profile = bodyStr(body, "level")
		// Map mdadm-style levels to btrfs profiles
		switch profile {
		case "1":
			profile = "raid1"
		case "10":
			profile = "raid10"
		case "0":
			profile = "raid0"
		case "":
			profile = "single"
		}
	}

	if name == "" || !regexp.MustCompile(`^[a-zA-Z0-9-]{1,32}$`).MatchString(name) {
		return map[string]interface{}{"error": "Invalid pool name. Use alphanumeric + hyphens, max 32 chars."}
	}
	reserved := map[string]bool{"system": true, "config": true, "temp": true, "swap": true, "root": true, "boot": true}
	if reserved[strings.ToLower(name)] {
		return map[string]interface{}{"error": fmt.Sprintf(`"%s" is a reserved name.`, name)}
	}

	// Check storage.json
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == name {
			return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" already exists.`, name)}
		}
	}

	// Parse disks
	disksRaw, _ := body["disks"].([]interface{})
	if len(disksRaw) < 1 {
		return map[string]interface{}{"error": "At least 1 disk required."}
	}
	var disks []string
	for _, d := range disksRaw {
		if ds, ok := d.(string); ok {
			if !strings.HasPrefix(ds, "/dev/") {
				ds = "/dev/" + ds
			}
			disks = append(disks, ds)
		}
	}

	// Validate profile vs disk count
	minDisks := map[string]int{"single": 1, "raid0": 2, "raid1": 2, "raid10": 4}
	if min, ok := minDisks[profile]; ok {
		if len(disks) < min {
			return map[string]interface{}{"error": fmt.Sprintf("%s requires at least %d disks. You selected %d.", profile, min, len(disks))}
		}
	} else {
		return map[string]interface{}{"error": fmt.Sprintf("Invalid profile: %s. Use single, raid0, raid1, or raid10.", profile)}
	}

	// Validate disks are eligible
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

	mountPoint := nimbusPoolsDir + "/" + name

	// ── Wipe disks ──
	for _, disk := range disks {
		run(fmt.Sprintf("wipefs -af %s 2>/dev/null || true", disk))
		run(fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 conv=notrunc 2>/dev/null || true", disk))
		run(fmt.Sprintf("partprobe %s 2>/dev/null || true", disk))
	}
	time.Sleep(1 * time.Second)

	// ── Create Btrfs filesystem ──
	// mkfs.btrfs -f -L nimbus-<name> -d <profile> -m <profile> <disks...>
	dataProfile := profile
	metaProfile := profile
	if profile == "single" && len(disks) == 1 {
		dataProfile = "single"
		metaProfile = "dup" // metadata always redundant even on single disk
	}

	args := []string{"mkfs.btrfs", "-f", "-L", "nimbus-" + name}
	args = append(args, "-d", dataProfile)
	args = append(args, "-m", metaProfile)
	args = append(args, disks...)

	logMsg("Btrfs: %s", strings.Join(args, " "))
	cmd := strings.Join(args, " ")
	out, ok := run(cmd + " 2>&1")
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("mkfs.btrfs failed: %s", out)}
	}

	// ── Mount ──
	os.MkdirAll(mountPoint, 0755)
	mountOpts := "defaults,noatime,compress=zstd:3"

	// Add to fstab FIRST so mount can use it
	uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", disks[0]))
	uuid = strings.TrimSpace(uuid)
	if uuid != "" {
		appendBtrfsFstab(uuid, mountPoint, mountOpts)
		run("systemctl daemon-reload 2>/dev/null || true")
		time.Sleep(500 * time.Millisecond)
	}

	// Try mount — multiple methods
	mounted := false

	// Method 1: mount by device
	mountCmd := exec.Command("mount", "-t", "btrfs", "-o", mountOpts, disks[0], mountPoint)
	mountResult, mountErr := mountCmd.CombinedOutput()
	if mountErr == nil {
		mounted = true
		logMsg("Btrfs mount OK (by device): %s → %s", disks[0], mountPoint)
	} else {
		logMsg("Btrfs mount by device failed: %s (exit: %v)", string(mountResult), mountErr)
	}

	// Method 2: mount by fstab entry
	if !mounted && uuid != "" {
		mountCmd2 := exec.Command("mount", mountPoint)
		mountResult2, mountErr2 := mountCmd2.CombinedOutput()
		if mountErr2 == nil {
			mounted = true
			logMsg("Btrfs mount OK (by fstab): %s", mountPoint)
		} else {
			logMsg("Btrfs mount by fstab failed: %s (exit: %v)", string(mountResult2), mountErr2)
		}
	}

	// Method 3: mount by label
	if !mounted {
		label := "nimbus-" + name
		mountCmd3 := exec.Command("mount", "-t", "btrfs", "-o", mountOpts, "LABEL="+label, mountPoint)
		mountResult3, mountErr3 := mountCmd3.CombinedOutput()
		if mountErr3 == nil {
			mounted = true
			logMsg("Btrfs mount OK (by label): %s → %s", label, mountPoint)
		} else {
			logMsg("Btrfs mount by label failed: %s (exit: %v)", string(mountResult3), mountErr3)
		}
	}

	if !mounted {
		return map[string]interface{}{"error": "Btrfs created but could not mount. Check system logs."}
	}

	// Verify mount
	verifyOut, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if strings.TrimSpace(verifyOut) == "" {
		return map[string]interface{}{"error": "Btrfs created but mount verification failed"}
	}

	// ── Create subvolumes ──
	// Btrfs subvolumes are like ZFS datasets — lightweight, can be snapshotted individually
	run(fmt.Sprintf("btrfs subvolume create %s/shares 2>/dev/null || true", mountPoint))
	run(fmt.Sprintf("btrfs subvolume create %s/docker 2>/dev/null || true", mountPoint))
	run(fmt.Sprintf("btrfs subvolume create %s/system-backup 2>/dev/null || true", mountPoint))

	// ── Create standard dirs ──
	createPoolDirs(mountPoint)

	// ── Write identity file ──
	writePoolIdentityBtrfs(mountPoint, name, profile, disks)

	// ── Save to storage.json ──
	conf = getStorageConfigFull()
	confPools, _ = conf["pools"].([]interface{})
	isFirst := len(confPools) == 0
	confPools = append(confPools, map[string]interface{}{
		"name":       name,
		"type":       "btrfs",
		"profile":    profile,
		"mountPoint": mountPoint,
		"disks":      disksRaw,
		"createdAt":  time.Now().UTC().Format(time.RFC3339),
	})
	conf["pools"] = confPools
	if isFirst {
		conf["primaryPool"] = name
		conf["configuredAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	saveStorageConfigFull(conf)

	logMsg("Btrfs pool '%s' created (%s, %d disks)", name, profile, len(disks))
	return map[string]interface{}{
		"ok":          true,
		"pool":        map[string]interface{}{"name": name, "type": "btrfs", "profile": profile, "mountPoint": mountPoint, "disks": disks},
		"isFirstPool": isFirst,
	}
}

// ═══════════════════════════════════
// Btrfs Pool Destroy
// ═══════════════════════════════════

func destroyPoolBtrfs(poolName string) map[string]interface{} {
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
	poolType, _ := poolConf["type"].(string)
	if poolType != "btrfs" {
		return map[string]interface{}{"error": "Not a Btrfs pool"}
	}

	mountPoint, _ := poolConf["mountPoint"].(string)

	// Get disks
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

	logMsg("Destroying Btrfs pool '%s' (mount: %s, disks: %v)", poolName, mountPoint, poolDisks)

	// ── 1. Delete shares from DB ──
	shares, _ := dbSharesList()
	for _, s := range shares {
		sharPool, _ := s["pool"].(string)
		sharVolume, _ := s["volume"].(string)
		sharPath, _ := s["path"].(string)
		sharName, _ := s["name"].(string)
		if sharPool == poolName || sharVolume == poolName || (mountPoint != "" && strings.HasPrefix(sharPath, mountPoint)) {
			handleOp(Request{Op: "share.delete", ShareName: sharName})
			dbSharesDelete(sharName)
		}
	}

	// ── 2. Clean Docker if on this pool ──
	dockerConf := getDockerConfigGo()
	dockerPath, _ := dockerConf["path"].(string)
	if dockerPath != "" && mountPoint != "" && strings.HasPrefix(dockerPath, mountPoint) {
		run("docker stop $(docker ps -aq) 2>/dev/null || true")
		run("docker rm $(docker ps -aq) 2>/dev/null || true")
		run("systemctl stop docker.socket docker containerd 2>/dev/null || true")
		run("systemctl disable docker.socket docker.service containerd.service 2>/dev/null || true")
		run("rm -rf /var/lib/docker 2>/dev/null || true")
		run("rm -f /etc/docker/daemon.json 2>/dev/null || true")
		saveDockerConfigGo(map[string]interface{}{
			"installed": false, "path": nil, "permissions": []interface{}{},
			"appPermissions": map[string]interface{}{},
		})
		saveInstalledApps([]map[string]interface{}{})
		run("groupdel nimos-share-docker-apps 2>/dev/null || true")
	}

	// ── 3. Kill processes and unmount submounts ──
	if mountPoint != "" {
		run(fmt.Sprintf("fuser -km %s 2>/dev/null || true", mountPoint))
		// Unmount all submounts (overlay, bind, etc.) in reverse order
		mountsOut, _ := run(fmt.Sprintf("findmnt -rn -o TARGET %s 2>/dev/null", mountPoint))
		mounts := strings.Split(strings.TrimSpace(mountsOut), "\n")
		for i := len(mounts) - 1; i >= 0; i-- {
			m := strings.TrimSpace(mounts[i])
			if m != "" && m != mountPoint {
				run(fmt.Sprintf("umount -l %s 2>/dev/null || true", m))
			}
		}
		run(fmt.Sprintf("umount -l %s 2>/dev/null || true", mountPoint))
		run(fmt.Sprintf("umount -f %s 2>/dev/null || true", mountPoint))
	}

	// Kill processes on disks and force unmount
	for _, disk := range poolDisks {
		run(fmt.Sprintf("fuser -km %s 2>/dev/null || true", disk))
	}

	// Wait and verify unmount completed
	time.Sleep(2 * time.Second)
	if mountPoint != "" {
		if out, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint)); strings.TrimSpace(out) != "" {
			// Still mounted — force harder
			run(fmt.Sprintf("umount -f %s 2>/dev/null || true", mountPoint))
			time.Sleep(1 * time.Second)
		}
	}

	// ── 4. Wipe filesystem signatures ──
	for _, disk := range poolDisks {
		run(fmt.Sprintf("wipefs -af %s 2>/dev/null || true", disk))
		run(fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=10 conv=notrunc 2>/dev/null || true", disk))
	}

	// ── 5. Remove mount point ──
	if mountPoint != "" && strings.HasPrefix(mountPoint, nimbusPoolsDir) {
		os.RemoveAll(mountPoint)
	}

	// ── 6. Clean fstab ──
	if fstab, err := os.ReadFile("/etc/fstab"); err == nil {
		var cleanLines []string
		for _, line := range strings.Split(string(fstab), "\n") {
			if mountPoint != "" && strings.Contains(line, mountPoint) {
				continue
			}
			if strings.Contains(line, "nimbus-"+poolName) {
				continue
			}
			cleanLines = append(cleanLines, line)
		}
		os.WriteFile("/etc/fstab", []byte(strings.Join(cleanLines, "\n")), 0644)
	}

	// ── 7. Remove from storage.json ──
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

	// ── 8. Rescan ──
	for _, disk := range poolDisks {
		run(fmt.Sprintf("partx -d %s 2>/dev/null || true", disk))
		run(fmt.Sprintf("blockdev --rereadpt %s 2>/dev/null || true", disk))
	}
	run("partprobe 2>/dev/null || true")
	rescanSCSIBuses()

	logMsg("Btrfs pool '%s' fully destroyed", poolName)
	return map[string]interface{}{"ok": true, "pool": poolName}
}

// ═══════════════════════════════════
// Btrfs Pool Status
// ═══════════════════════════════════

func getBtrfsPoolStatus(mountPoint string) map[string]interface{} {
	result := map[string]interface{}{
		"status": "unknown", "total": int64(0), "used": int64(0), "available": int64(0),
	}

	// Check if mounted
	mountSrc, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if !ok || strings.TrimSpace(mountSrc) == "" {
		result["status"] = "offline"
		return result
	}
	rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
	if strings.TrimSpace(mountSrc) == strings.TrimSpace(rootSrc) {
		result["status"] = "offline"
		return result
	}

	// Get usage from df
	if dfOut, ok := run(fmt.Sprintf("df -B1 --output=size,used,avail %s 2>/dev/null", mountPoint)); ok {
		lines := strings.Split(strings.TrimSpace(dfOut), "\n")
		if len(lines) > 1 {
			parts := strings.Fields(lines[1])
			if len(parts) >= 3 {
				result["total"] = parseInt64(parts[0])
				result["used"] = parseInt64(parts[1])
				result["available"] = parseInt64(parts[2])
			}
		}
	}

	// Get device stats for error checking
	errOut, _ := run(fmt.Sprintf("btrfs device stats %s 2>/dev/null", mountPoint))
	hasErrors := false
	if errOut != "" {
		for _, line := range strings.Split(errOut, "\n") {
			// Lines like: [/dev/sda].write_io_errs    0
			if strings.Contains(line, "_errs") && !strings.HasSuffix(strings.TrimSpace(line), "0") {
				hasErrors = true
				break
			}
		}
	}
	result["errors"] = hasErrors

	// Check device status
	devOut, _ := run(fmt.Sprintf("btrfs filesystem show %s 2>/dev/null", mountPoint))
	if strings.Contains(devOut, "missing") {
		result["status"] = "degraded"
	} else if hasErrors {
		result["status"] = "errors"
	} else {
		result["status"] = "active"
	}

	return result
}

func getBtrfsPoolInfo(poolConf map[string]interface{}, primaryPool string) map[string]interface{} {
	poolName, _ := poolConf["name"].(string)
	mountPoint, _ := poolConf["mountPoint"].(string)
	profile, _ := poolConf["profile"].(string)
	createdAt, _ := poolConf["createdAt"].(string)

	status := getBtrfsPoolStatus(mountPoint)
	total, _ := status["total"].(int64)
	used, _ := status["used"].(int64)
	available, _ := status["available"].(int64)
	poolStatus, _ := status["status"].(string)

	var disks []interface{}
	if d, ok := poolConf["disks"].([]interface{}); ok {
		disks = d
	}
	if disks == nil {
		disks = []interface{}{}
	}

	usagePct := 0
	if total > 0 {
		usagePct = int(math.Round(float64(used) / float64(total) * 100))
	}

	return map[string]interface{}{
		"name":               poolName,
		"type":               "btrfs",
		"profile":            profile,
		"mountPoint":         mountPoint,
		"raidLevel":          profile,
		"filesystem":         "btrfs",
		"createdAt":          createdAt,
		"disks":              disks,
		"status":             poolStatus,
		"errors":             status["errors"],
		"total":              total,
		"used":               used,
		"available":          available,
		"totalFormatted":     formatBytes(total),
		"usedFormatted":      formatBytes(used),
		"availableFormatted": formatBytes(available),
		"usagePercent":       usagePct,
		"isPrimary":          poolName == primaryPool,
	}
}

// ═══════════════════════════════════
// Btrfs Subvolumes (like ZFS datasets)
// ═══════════════════════════════════

func listBtrfsSubvolumes(mountPoint string) []map[string]interface{} {
	out, ok := run(fmt.Sprintf("btrfs subvolume list -o %s 2>/dev/null", mountPoint))
	if !ok || out == "" {
		return []map[string]interface{}{}
	}

	var subvols []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		// Format: ID 256 gen 7 top level 5 path shares
		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue
		}
		subvolPath := parts[len(parts)-1]
		subvols = append(subvols, map[string]interface{}{
			"name":      filepath.Base(subvolPath),
			"path":      subvolPath,
			"fullPath":  filepath.Join(mountPoint, subvolPath),
		})
	}
	if subvols == nil {
		subvols = []map[string]interface{}{}
	}
	return subvols
}

// ═══════════════════════════════════
// Btrfs Snapshots
// ═══════════════════════════════════

func listBtrfsSnapshots(mountPoint string) []map[string]interface{} {
	out, ok := run(fmt.Sprintf("btrfs subvolume list -s %s 2>/dev/null", mountPoint))
	if !ok || out == "" {
		return []map[string]interface{}{}
	}

	var snaps []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 11 {
			continue
		}
		snapPath := parts[len(parts)-1]
		// Extract creation time (fields 10-13 roughly)
		created := ""
		for i, p := range parts {
			if p == "otime" && i+1 < len(parts) {
				created = strings.Join(parts[i+1:], " ")
				break
			}
		}

		snaps = append(snaps, map[string]interface{}{
			"name":     filepath.Base(snapPath),
			"path":     snapPath,
			"fullPath": filepath.Join(mountPoint, snapPath),
			"created":  created,
		})
	}
	if snaps == nil {
		snaps = []map[string]interface{}{}
	}
	return snaps
}

func createBtrfsSnapshot(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	subvolume := bodyStr(body, "subvolume") // e.g. "shares"
	snapName := bodyStr(body, "name")

	if pool == "" || subvolume == "" {
		return map[string]interface{}{"error": "Pool and subvolume required"}
	}

	mountPoint := resolveBtrfsMountPoint(pool)
	if mountPoint == "" {
		return map[string]interface{}{"error": "Pool not found"}
	}

	if snapName == "" {
		snapName = "manual-" + time.Now().Format("20060102-150405")
	}

	srcPath := filepath.Join(mountPoint, subvolume)
	snapDir := filepath.Join(mountPoint, ".snapshots")
	os.MkdirAll(snapDir, 0755)
	snapPath := filepath.Join(snapDir, subvolume+"-"+snapName)

	out, ok := run(fmt.Sprintf("btrfs subvolume snapshot -r %s %s 2>&1", srcPath, snapPath))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Snapshot failed: %s", out)}
	}

	logMsg("Btrfs snapshot created: %s → %s", srcPath, snapPath)
	return map[string]interface{}{"ok": true, "snapshot": snapPath}
}

func destroyBtrfsSnapshot(body map[string]interface{}) map[string]interface{} {
	snapshot := bodyStr(body, "snapshot") // full path
	if snapshot == "" {
		return map[string]interface{}{"error": "Snapshot path required"}
	}

	// Safety: must be within a pool's .snapshots directory
	if !strings.Contains(snapshot, ".snapshots") || !strings.HasPrefix(snapshot, nimbusPoolsDir) {
		return map[string]interface{}{"error": "Invalid snapshot path"}
	}

	out, ok := run(fmt.Sprintf("btrfs subvolume delete %s 2>&1", snapshot))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Failed: %s", out)}
	}

	return map[string]interface{}{"ok": true}
}

// ═══════════════════════════════════
// Btrfs Scrub
// ═══════════════════════════════════

func startBtrfsScrub(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	mountPoint := resolveBtrfsMountPoint(pool)
	if mountPoint == "" {
		return map[string]interface{}{"error": "Pool not found"}
	}

	out, ok := run(fmt.Sprintf("btrfs scrub start %s 2>&1", mountPoint))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Scrub failed: %s", out)}
	}

	logMsg("Btrfs scrub started on %s", mountPoint)
	return map[string]interface{}{"ok": true}
}

func getBtrfsScrubStatus(mountPoint string) map[string]interface{} {
	out, ok := run(fmt.Sprintf("btrfs scrub status %s 2>/dev/null", mountPoint))
	if !ok {
		return map[string]interface{}{"status": "unknown"}
	}

	result := map[string]interface{}{"status": "none"}
	if strings.Contains(out, "running") {
		result["status"] = "running"
	} else if strings.Contains(out, "finished") {
		result["status"] = "completed"
		if strings.Contains(out, "with 0 errors") || strings.Contains(out, "no errors") {
			result["errors"] = 0
		}
	}
	return result
}

// ═══════════════════════════════════
// Btrfs Helpers
// ═══════════════════════════════════

func resolveBtrfsMountPoint(poolName string) string {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			mp, _ := pm["mountPoint"].(string)
			return mp
		}
	}
	return ""
}

func appendBtrfsFstab(uuid, mountPoint, opts string) {
	existing, _ := os.ReadFile("/etc/fstab")
	if strings.Contains(string(existing), mountPoint) {
		return
	}
	entry := fmt.Sprintf("UUID=%s %s btrfs %s 0 0\n", uuid, mountPoint, opts)
	f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(entry)
}

func writePoolIdentityBtrfs(mountPoint, name, profile string, disks []string) {
	identity := map[string]interface{}{
		"name":          name,
		"type":          "btrfs",
		"profile":       profile,
		"disks":         disks,
		"createdAt":     time.Now().UTC().Format(time.RFC3339),
		"nimbusVersion": "5.0.0-beta",
	}
	data, _ := json.MarshalIndent(identity, "", "  ")
	os.WriteFile(filepath.Join(mountPoint, ".nimbus-pool.json"), data, 0644)
}

// ═══════════════════════════════════
// Btrfs HTTP Routes
// ═══════════════════════════════════

func handleBtrfsRoutes(w http.ResponseWriter, r *http.Request, method, urlPath string, session map[string]interface{}, body map[string]interface{}) bool {
	if method == "GET" {
		switch urlPath {
		case "/api/storage/btrfs/subvolumes":
			pool := r.URL.Query().Get("pool")
			mp := resolveBtrfsMountPoint(pool)
			if mp == "" {
				jsonError(w, 400, "Pool not found")
			} else {
				jsonOk(w, map[string]interface{}{"subvolumes": listBtrfsSubvolumes(mp)})
			}
			return true
		case "/api/storage/btrfs/snapshots":
			pool := r.URL.Query().Get("pool")
			mp := resolveBtrfsMountPoint(pool)
			if mp == "" {
				jsonError(w, 400, "Pool not found")
			} else {
				jsonOk(w, map[string]interface{}{"snapshots": listBtrfsSnapshots(mp)})
			}
			return true
		case "/api/storage/btrfs/scrub":
			pool := r.URL.Query().Get("pool")
			mp := resolveBtrfsMountPoint(pool)
			if mp == "" {
				jsonError(w, 400, "Pool not found")
			} else {
				jsonOk(w, map[string]interface{}{"scrub": getBtrfsScrubStatus(mp)})
			}
			return true
		}
		return false
	}

	if method == "POST" {
		switch urlPath {
		case "/api/storage/btrfs/snapshot":
			jsonOk(w, createBtrfsSnapshot(body))
			return true
		case "/api/storage/btrfs/scrub":
			jsonOk(w, startBtrfsScrub(body))
			return true
		}
		return false
	}

	if method == "DELETE" {
		switch urlPath {
		case "/api/storage/btrfs/snapshot":
			jsonOk(w, destroyBtrfsSnapshot(body))
			return true
		}
		return false
	}

	return false
}

// ═══════════════════════════════════
// Auto-mount on startup
// ═══════════════════════════════════

func btrfsAutoMountOnStartup() {
	if !hasBtrfs {
		return
	}

	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		poolType, _ := pm["type"].(string)
		if poolType != "btrfs" {
			continue
		}
		mountPoint, _ := pm["mountPoint"].(string)
		if mountPoint == "" {
			continue
		}

		// Check if already mounted
		mountSrc, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
		if strings.TrimSpace(mountSrc) != "" {
			continue // already mounted
		}

		// Try mounting from fstab
		os.MkdirAll(mountPoint, 0755)
		run(fmt.Sprintf("mount %s 2>/dev/null || true", mountPoint))
	}

	logMsg("Btrfs auto-mount completed")
}
