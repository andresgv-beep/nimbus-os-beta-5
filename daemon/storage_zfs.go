package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ═══════════════════════════════════
// ZFS Pool Operations
// ═══════════════════════════════════

func createPoolZfs(body map[string]interface{}) map[string]interface{} {
	name := bodyStr(body, "name")
	vdevType := bodyStr(body, "vdevType") // mirror, raidz1, raidz2, stripe
	if vdevType == "" {
		vdevType = bodyStr(body, "level") // fallback from frontend
	}

	if name == "" || !regexp.MustCompile(`^[a-zA-Z0-9-]{1,32}$`).MatchString(name) {
		return map[string]interface{}{"error": "Invalid pool name. Use alphanumeric + hyphens, max 32 chars."}
	}
	reserved := map[string]bool{"system": true, "config": true, "temp": true, "swap": true, "root": true, "boot": true, "rpool": true}
	if reserved[strings.ToLower(name)] {
		return map[string]interface{}{"error": fmt.Sprintf(`"%s" is a reserved name.`, name)}
	}

	// Check if zpool already exists
	if out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", "nimos-"+name)); strings.TrimSpace(out) != "" {
		return map[string]interface{}{"error": fmt.Sprintf(`ZFS pool "nimos-%s" already exists.`, name)}
	}

	// Check storage.json too
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == name {
			return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" already exists in config.`, name)}
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

	// Validate vdev type vs disk count
	minDisks := map[string]int{"stripe": 1, "mirror": 2, "raidz1": 3, "raidz2": 4, "raidz3": 5}
	if min, ok := minDisks[vdevType]; ok {
		if len(disks) < min {
			return map[string]interface{}{"error": fmt.Sprintf("%s requires at least %d disks. You selected %d.", vdevType, min, len(disks))}
		}
	} else if vdevType != "" {
		return map[string]interface{}{"error": fmt.Sprintf("Invalid vdev type: %s. Use mirror, raidz1, raidz2, or stripe.", vdevType)}
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

	zpoolName := "nimos-" + name
	mountPoint := nimbusPoolsDir + "/" + name

	// Wipe disks
	for _, disk := range disks {
		runExec("wipefs", "-af", disk)
		runExec("sgdisk", "-Z", disk)
		runExec("dd", "if=/dev/zero", "of="+disk, "bs=1M", "count=10")
		runExec("partprobe", disk)
	}
	time.Sleep(2 * time.Second)

	// Build zpool create command
	// zpool create [-f] <poolname> <vdevtype> <disks...>
	args := []string{"create", "-f", "-o", "ashift=12"}
	// Mount point
	args = append(args, "-m", mountPoint)
	args = append(args, zpoolName)

	if vdevType == "stripe" || len(disks) == 1 {
		// No vdev keyword for stripe/single
		args = append(args, disks...)
	} else {
		args = append(args, vdevType)
		args = append(args, disks...)
	}

	logMsg("ZFS: zpool create %s", strings.Join(args[1:], " "))
	cmd := fmt.Sprintf("zpool %s 2>&1", strings.Join(args, " "))
	out, ok := run(cmd)
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("zpool create failed: %s", out)}
	}

	// Set pool properties
	run(fmt.Sprintf("zfs set compression=lz4 %s 2>/dev/null", zpoolName))
	run(fmt.Sprintf("zfs set atime=off %s 2>/dev/null", zpoolName))
	run(fmt.Sprintf("zfs set xattr=sa %s 2>/dev/null", zpoolName))
	run(fmt.Sprintf("zfs set acltype=posixacl %s 2>/dev/null", zpoolName))

	// Create standard datasets
	run(fmt.Sprintf("zfs create %s/shares 2>/dev/null", zpoolName))
	run(fmt.Sprintf("zfs create %s/docker 2>/dev/null", zpoolName))
	run(fmt.Sprintf("zfs create %s/system-backup 2>/dev/null", zpoolName))

	// Create pool dirs (for compatibility)
	os.MkdirAll(filepath.Join(mountPoint, "docker", "containers"), 0755)
	os.MkdirAll(filepath.Join(mountPoint, "docker", "stacks"), 0755)
	os.MkdirAll(filepath.Join(mountPoint, "docker", "volumes"), 0755)

	// Write identity file
	writePoolIdentityZfs(mountPoint, name, vdevType, disks)

	// Save to storage.json
	conf = getStorageConfigFull()
	confPools, _ = conf["pools"].([]interface{})
	isFirst := len(confPools) == 0
	confPools = append(confPools, map[string]interface{}{
		"name":       name,
		"type":       "zfs",
		"zpoolName":  zpoolName,
		"mountPoint": mountPoint,
		"vdevType":   vdevType,
		"disks":      disksRaw,
		"createdAt":  time.Now().UTC().Format(time.RFC3339),
	})
	conf["pools"] = confPools
	if isFirst {
		conf["primaryPool"] = name
		conf["configuredAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	saveStorageConfigFull(conf)

	logMsg("ZFS pool '%s' created successfully (%s, %d disks)", name, vdevType, len(disks))

	return map[string]interface{}{
		"ok":          true,
		"pool":        map[string]interface{}{"name": name, "type": "zfs", "zpoolName": zpoolName, "mountPoint": mountPoint, "vdevType": vdevType, "disks": disks},
		"isFirstPool": isFirst,
	}
}

func destroyPoolZfs(poolName string) map[string]interface{} {
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
	if poolType != "zfs" {
		return map[string]interface{}{"error": "Not a ZFS pool"}
	}

	zpoolName, _ := poolConf["zpoolName"].(string)
	if zpoolName == "" {
		zpoolName = "nimos-" + poolName
	}
	mountPoint, _ := poolConf["mountPoint"].(string)

	logMsg("Destroying ZFS pool '%s' (zpool: %s, mount: %s)", poolName, zpoolName, mountPoint)

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

	// ── 2. Stop Docker if on this pool ──
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
		logMsg("Docker stopped and config cleaned (was on pool '%s')", poolName)
	}

	// ── 3. Unmount any overlay/bind mounts on the pool ──
	if mountPoint != "" {
		// Kill any processes using the pool
		run(fmt.Sprintf("fuser -km %s 2>/dev/null || true", mountPoint))
		// Unmount all submounts (overlay, bind, etc.)
		mountsOut, _ := run(fmt.Sprintf("findmnt -rn -o TARGET %s 2>/dev/null", mountPoint))
		// Reverse order to unmount children first
		mounts := strings.Split(strings.TrimSpace(mountsOut), "\n")
		for i := len(mounts) - 1; i >= 0; i-- {
			m := strings.TrimSpace(mounts[i])
			if m != "" && m != mountPoint {
				run(fmt.Sprintf("umount -l %s 2>/dev/null || true", m))
			}
		}
	}

	// ── 4. Destroy the zpool ──
	out, ok := run(fmt.Sprintf("zpool destroy -f %s 2>&1", zpoolName))
	if !ok {
		// One more try after a short wait
		time.Sleep(2 * time.Second)
		out, ok = run(fmt.Sprintf("zpool destroy -f %s 2>&1", zpoolName))
		if !ok {
			return map[string]interface{}{"error": fmt.Sprintf("zpool destroy failed: %s", out)}
		}
	}

	// ── 5. Remove mount point directory ──
	if mountPoint != "" && strings.HasPrefix(mountPoint, nimbusPoolsDir) {
		os.RemoveAll(mountPoint)
	}

	// ── 6. Remove from storage.json ──
	confPools = append(confPools[:poolIdx], confPools[poolIdx+1:]...)
	conf["pools"] = confPools
	if primary, _ := conf["primaryPool"].(string); primary == poolName {
		if len(confPools) > 0 {
			if first, ok := confPools[0].(map[string]interface{}); ok {
				conf["primaryPool"] = first["name"]
			}
		} else {
			conf["primaryPool"] = nil
			conf["configuredAt"] = nil
		}
	}
	saveStorageConfigFull(conf)

	// ── 7. Rescan disks ──
	run("partprobe 2>/dev/null || true")
	rescanSCSIBuses()

	logMsg("ZFS pool '%s' fully destroyed", poolName)
	return map[string]interface{}{"ok": true}
}

// ═══════════════════════════════════
// ZFS Pool Import (recovery)
// ═══════════════════════════════════

func listImportableZfsPools() []map[string]interface{} {
	out, ok := run("zpool import 2>&1")
	if !ok || out == "" || strings.Contains(out, "no pools available") {
		return []map[string]interface{}{}
	}

	var pools []map[string]interface{}
	var current map[string]interface{}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pool:") {
			if current != nil {
				pools = append(pools, current)
			}
			current = map[string]interface{}{
				"zpoolName": strings.TrimSpace(strings.TrimPrefix(line, "pool:")),
				"status":    "unknown",
				"disks":     []interface{}{},
			}
		} else if current != nil {
			if strings.HasPrefix(line, "state:") {
				current["status"] = strings.TrimSpace(strings.TrimPrefix(line, "state:"))
			} else if strings.HasPrefix(line, "id:") {
				current["id"] = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			}
		}
	}
	if current != nil {
		pools = append(pools, current)
	}

	// Enrich with nimos- prefix detection
	for _, p := range pools {
		zpoolName, _ := p["zpoolName"].(string)
		if strings.HasPrefix(zpoolName, "nimos-") {
			p["nimosName"] = strings.TrimPrefix(zpoolName, "nimos-")
			p["isNimosPool"] = true
		} else {
			p["nimosName"] = zpoolName
			p["isNimosPool"] = false
		}
	}

	return pools
}

func importPoolZfs(body map[string]interface{}) map[string]interface{} {
	zpoolName := bodyStr(body, "zpoolName")
	customName := bodyStr(body, "name") // optional rename

	if zpoolName == "" {
		return map[string]interface{}{"error": "zpoolName required"}
	}

	// Determine NimOS name
	nimosName := customName
	if nimosName == "" {
		if strings.HasPrefix(zpoolName, "nimos-") {
			nimosName = strings.TrimPrefix(zpoolName, "nimos-")
		} else {
			nimosName = zpoolName
		}
	}

	mountPoint := nimbusPoolsDir + "/" + nimosName
	os.MkdirAll(mountPoint, 0755)

	// Import the pool
	out, ok := run(fmt.Sprintf("zpool import -f %s 2>&1", zpoolName))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("zpool import failed: %s", out)}
	}

	// Set mount point
	run(fmt.Sprintf("zfs set mountpoint=%s %s 2>/dev/null", mountPoint, zpoolName))

	// Get pool info
	vdevType := detectZfsVdevType(zpoolName)
	disks := getZfsPoolDisks(zpoolName)

	// Check for existing identity file
	identityPath := filepath.Join(mountPoint, ".nimbus-pool.json")
	if _, err := os.Stat(identityPath); err != nil {
		// No identity file — create one
		writePoolIdentityZfs(mountPoint, nimosName, vdevType, disks)
	}

	// Save to config
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	// Check not already configured
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == nimosName {
			return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" already configured`, nimosName)}
		}
	}

	isFirst := len(confPools) == 0
	confPools = append(confPools, map[string]interface{}{
		"name":       nimosName,
		"type":       "zfs",
		"zpoolName":  zpoolName,
		"mountPoint": mountPoint,
		"vdevType":   vdevType,
		"disks":      disks,
		"createdAt":  time.Now().UTC().Format(time.RFC3339),
		"imported":   true,
	})
	conf["pools"] = confPools
	if isFirst {
		conf["primaryPool"] = nimosName
	}
	saveStorageConfigFull(conf)

	logMsg("ZFS pool '%s' imported as '%s'", zpoolName, nimosName)
	return map[string]interface{}{"ok": true, "pool": map[string]interface{}{"name": nimosName, "zpoolName": zpoolName, "mountPoint": mountPoint}}
}

// ═══════════════════════════════════
// ZFS Pool Status
// ═══════════════════════════════════

func getZfsPoolStatus(zpoolName string) map[string]interface{} {
	result := map[string]interface{}{
		"status": "unknown", "health": "unknown",
		"total": int64(0), "used": int64(0), "available": int64(0),
		"scrub": nil, "errors": "",
	}

	// Basic info: name, size, alloc, free, health
	out, ok := run(fmt.Sprintf("zpool list -Hp -o name,size,alloc,free,health %s 2>/dev/null", zpoolName))
	if !ok || out == "" {
		result["status"] = "missing"
		return result
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) >= 5 {
		result["total"] = parseInt64(parts[1])
		result["used"] = parseInt64(parts[2])
		result["available"] = parseInt64(parts[3])
		result["health"] = parts[4]
		health := strings.ToUpper(parts[4])
		switch health {
		case "ONLINE":
			result["status"] = "active"
		case "DEGRADED":
			result["status"] = "degraded"
		case "FAULTED":
			result["status"] = "faulted"
		case "SUSPENDED":
			result["status"] = "suspended"
		default:
			result["status"] = strings.ToLower(health)
		}
	}

	// Scrub status
	scrubOut, _ := run(fmt.Sprintf("zpool status %s 2>/dev/null | grep -A2 'scan:'", zpoolName))
	if scrubOut != "" {
		scrub := map[string]interface{}{"status": "none"}
		if strings.Contains(scrubOut, "in progress") {
			re := regexp.MustCompile(`(\d+\.\d+)%\s+done`)
			if m := re.FindStringSubmatch(scrubOut); m != nil {
				pct, _ := strconv.ParseFloat(m[1], 64)
				scrub["status"] = "running"
				scrub["progress"] = pct
			}
		} else if strings.Contains(scrubOut, "scrub repaired") || strings.Contains(scrubOut, "scrub canceled") {
			re := regexp.MustCompile(`on\s+(.+)`)
			if m := re.FindStringSubmatch(scrubOut); m != nil {
				scrub["lastRun"] = strings.TrimSpace(m[1])
			}
			if strings.Contains(scrubOut, "with 0 errors") {
				scrub["status"] = "completed"
				scrub["errors"] = 0
			} else {
				scrub["status"] = "completed_errors"
				reErr := regexp.MustCompile(`(\d+)\s+errors`)
				if m := reErr.FindStringSubmatch(scrubOut); m != nil {
					scrub["errors"] = parseIntDefault(m[1], 0)
				}
			}
		}
		result["scrub"] = scrub
	}

	return result
}

// ═══════════════════════════════════
// ZFS Datasets
// ═══════════════════════════════════

func listZfsDatasets(zpoolName string) []map[string]interface{} {
	// zfs list -H -o name,used,avail,refer,quota,compression,mountpoint -r <pool>/shares
	out, ok := run(fmt.Sprintf("zfs list -H -o name,used,avail,refer,quota,compression,mountpoint -r %s 2>/dev/null", zpoolName))
	if !ok || out == "" {
		return []map[string]interface{}{}
	}

	var datasets []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 7 {
			continue
		}
		fullName := parts[0]
		// Skip the root dataset itself
		if fullName == zpoolName {
			continue
		}

		// Parse relative name
		relName := strings.TrimPrefix(fullName, zpoolName+"/")

		datasets = append(datasets, map[string]interface{}{
			"name":        relName,
			"fullName":    fullName,
			"used":        parts[1],
			"available":   parts[2],
			"referenced":  parts[3],
			"quota":       parts[4],
			"compression": parts[5],
			"mountPoint":  parts[6],
		})
	}

	if datasets == nil {
		datasets = []map[string]interface{}{}
	}
	return datasets
}

func createZfsDataset(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	name := bodyStr(body, "name")
	quota := bodyStr(body, "quota")       // e.g. "50G", "none"
	compression := bodyStr(body, "compression") // lz4, zstd, off

	if pool == "" || name == "" {
		return map[string]interface{}{"error": "Pool and name required"}
	}
	if matched, _ := regexp.MatchString(`[^a-zA-Z0-9_\-]`, name); matched {
		return map[string]interface{}{"error": "Name can only contain letters, numbers, -, _"}
	}

	// Resolve zpool name
	zpoolName := resolveZpoolName(pool)
	if zpoolName == "" {
		return map[string]interface{}{"error": fmt.Sprintf("Pool '%s' not found", pool)}
	}

	fullName := zpoolName + "/shares/" + name

	// Check if exists
	if existing, _ := run(fmt.Sprintf("zfs list -H -o name %s 2>/dev/null", fullName)); strings.TrimSpace(existing) != "" {
		return map[string]interface{}{"error": fmt.Sprintf(`Dataset "%s" already exists`, name)}
	}

	// Ensure parent exists
	run(fmt.Sprintf("zfs create -p %s/shares 2>/dev/null", zpoolName))

	// Create
	out, ok := run(fmt.Sprintf("zfs create %s 2>&1", fullName))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Failed to create dataset: %s", out)}
	}

	// Set properties
	if quota != "" && quota != "none" && quota != "0" {
		run(fmt.Sprintf("zfs set quota=%s %s 2>/dev/null", quota, fullName))
	}
	if compression != "" && compression != "inherit" {
		run(fmt.Sprintf("zfs set compression=%s %s 2>/dev/null", compression, fullName))
	}

	logMsg("ZFS dataset '%s' created", fullName)
	return map[string]interface{}{"ok": true, "dataset": fullName}
}

func updateZfsDataset(body map[string]interface{}) map[string]interface{} {
	dataset := bodyStr(body, "dataset") // full ZFS name
	quota := bodyStr(body, "quota")
	compression := bodyStr(body, "compression")

	if dataset == "" {
		return map[string]interface{}{"error": "Dataset name required"}
	}

	if quota != "" {
		if quota == "none" || quota == "0" {
			run(fmt.Sprintf("zfs set quota=none %s 2>/dev/null", dataset))
		} else {
			run(fmt.Sprintf("zfs set quota=%s %s 2>/dev/null", quota, dataset))
		}
	}
	if compression != "" {
		run(fmt.Sprintf("zfs set compression=%s %s 2>/dev/null", compression, dataset))
	}

	return map[string]interface{}{"ok": true}
}

func destroyZfsDataset(body map[string]interface{}) map[string]interface{} {
	dataset := bodyStr(body, "dataset")
	if dataset == "" {
		return map[string]interface{}{"error": "Dataset name required"}
	}

	// Safety: don't destroy root or system datasets
	parts := strings.Split(dataset, "/")
	if len(parts) < 3 {
		return map[string]interface{}{"error": "Cannot destroy root dataset"}
	}

	out, ok := run(fmt.Sprintf("zfs destroy -r %s 2>&1", dataset))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Failed to destroy dataset: %s", out)}
	}

	logMsg("ZFS dataset '%s' destroyed", dataset)
	return map[string]interface{}{"ok": true}
}

// ═══════════════════════════════════
// ZFS Snapshots
// ═══════════════════════════════════

func listZfsSnapshots(dataset string) []map[string]interface{} {
	target := dataset
	if target == "" {
		return []map[string]interface{}{}
	}

	out, ok := run(fmt.Sprintf("zfs list -H -t snapshot -o name,used,creation -r %s 2>/dev/null", target))
	if !ok || out == "" {
		return []map[string]interface{}{}
	}

	var snaps []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		fullName := parts[0]
		// Extract snapshot name after @
		atIdx := strings.Index(fullName, "@")
		if atIdx < 0 {
			continue
		}
		snapName := fullName[atIdx+1:]
		datasetName := fullName[:atIdx]

		snaps = append(snaps, map[string]interface{}{
			"name":     snapName,
			"fullName": fullName,
			"dataset":  datasetName,
			"used":     strings.TrimSpace(parts[1]),
			"created":  strings.TrimSpace(parts[2]),
			"isAuto":   strings.HasPrefix(snapName, "auto-"),
		})
	}

	// Sort newest first
	sort.Slice(snaps, func(i, j int) bool {
		ci, _ := snaps[i]["created"].(string)
		cj, _ := snaps[j]["created"].(string)
		return ci > cj
	})

	if snaps == nil {
		snaps = []map[string]interface{}{}
	}
	return snaps
}

func createZfsSnapshot(body map[string]interface{}) map[string]interface{} {
	dataset := bodyStr(body, "dataset")
	snapName := bodyStr(body, "name")

	if dataset == "" {
		return map[string]interface{}{"error": "Dataset required"}
	}
	if snapName == "" {
		snapName = "manual-" + time.Now().Format("20060102-150405")
	}

	fullSnap := dataset + "@" + snapName
	out, ok := run(fmt.Sprintf("zfs snapshot %s 2>&1", fullSnap))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Snapshot failed: %s", out)}
	}

	logMsg("ZFS snapshot created: %s", fullSnap)
	return map[string]interface{}{"ok": true, "snapshot": fullSnap}
}

func rollbackZfsSnapshot(body map[string]interface{}) map[string]interface{} {
	snapshot := bodyStr(body, "snapshot") // full name: pool/dataset@snapname

	if snapshot == "" {
		return map[string]interface{}{"error": "Snapshot name required"}
	}
	if !strings.Contains(snapshot, "@") {
		return map[string]interface{}{"error": "Invalid snapshot format (need dataset@name)"}
	}

	// -r destroys newer snapshots
	out, ok := run(fmt.Sprintf("zfs rollback -r %s 2>&1", snapshot))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Rollback failed: %s", out)}
	}

	logMsg("ZFS rollback to: %s", snapshot)
	return map[string]interface{}{"ok": true}
}

func destroyZfsSnapshot(body map[string]interface{}) map[string]interface{} {
	snapshot := bodyStr(body, "snapshot")
	if snapshot == "" || !strings.Contains(snapshot, "@") {
		return map[string]interface{}{"error": "Valid snapshot name required"}
	}

	out, ok := run(fmt.Sprintf("zfs destroy %s 2>&1", snapshot))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Failed to destroy snapshot: %s", out)}
	}

	return map[string]interface{}{"ok": true}
}

// ═══════════════════════════════════
// ZFS Scrub
// ═══════════════════════════════════

func startZfsScrub(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	zpoolName := resolveZpoolName(pool)
	if zpoolName == "" {
		return map[string]interface{}{"error": "Pool not found"}
	}

	out, ok := run(fmt.Sprintf("zpool scrub %s 2>&1", zpoolName))
	if !ok {
		return map[string]interface{}{"error": fmt.Sprintf("Scrub failed: %s", out)}
	}

	logMsg("ZFS scrub started on %s", zpoolName)
	return map[string]interface{}{"ok": true}
}

// ═══════════════════════════════════
// ZFS Snapshot Scheduler
// ═══════════════════════════════════

func startZfsScheduler() {
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			conf := getStorageConfigFull()
			snapConf, _ := conf["snapshots"].(map[string]interface{})
			if snapConf == nil {
				continue
			}
			enabled, _ := snapConf["enabled"].(bool)
			if !enabled {
				continue
			}
			schedule, _ := snapConf["schedule"].(string)

			now := time.Now()
			shouldSnap := false

			switch schedule {
			case "hourly":
				shouldSnap = now.Minute() == 0
			case "daily":
				shouldSnap = now.Hour() == 3 && now.Minute() == 0 // 3 AM
			case "weekly":
				shouldSnap = now.Weekday() == time.Sunday && now.Hour() == 3 && now.Minute() == 0
			}

			if shouldSnap {
				runAutoSnapshots(conf)
			}
		}
	}()
}

func runAutoSnapshots(conf map[string]interface{}) {
	confPools, _ := conf["pools"].([]interface{})
	snapConf, _ := conf["snapshots"].(map[string]interface{})
	retention, _ := snapConf["retention"].(map[string]interface{})

	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		poolType, _ := pm["type"].(string)
		if poolType != "zfs" {
			continue
		}
		zpoolName, _ := pm["zpoolName"].(string)
		if zpoolName == "" {
			continue
		}

		// Snapshot all share datasets
		datasets := listZfsDatasets(zpoolName)
		for _, ds := range datasets {
			fullName, _ := ds["fullName"].(string)
			if !strings.Contains(fullName, "/shares/") {
				continue
			}

			snapName := "auto-" + time.Now().Format("20060102-1504")
			run(fmt.Sprintf("zfs snapshot %s@%s 2>/dev/null", fullName, snapName))
		}

		// Clean old snapshots
		if retention != nil {
			cleanAutoSnapshots(zpoolName, retention)
		}
	}
}

func cleanAutoSnapshots(zpoolName string, retention map[string]interface{}) {
	maxHourly := 24
	maxDaily := 30
	maxWeekly := 12

	if v, ok := retention["hourly"].(float64); ok {
		maxHourly = int(v)
	}
	if v, ok := retention["daily"].(float64); ok {
		maxDaily = int(v)
	}
	if v, ok := retention["weekly"].(float64); ok {
		maxWeekly = int(v)
	}

	out, _ := run(fmt.Sprintf("zfs list -H -t snapshot -o name -S creation -r %s 2>/dev/null", zpoolName))
	if out == "" {
		return
	}

	autoSnaps := []string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "@auto-") {
			autoSnaps = append(autoSnaps, strings.TrimSpace(line))
		}
	}

	// Simple retention: keep maxHourly most recent auto snapshots
	// More sophisticated hourly/daily/weekly bucketing can come later
	maxKeep := maxHourly + maxDaily + maxWeekly
	if len(autoSnaps) > maxKeep {
		for _, snap := range autoSnaps[maxKeep:] {
			run(fmt.Sprintf("zfs destroy %s 2>/dev/null", snap))
		}
	}
}

// ═══════════════════════════════════
// ZFS Helpers
// ═══════════════════════════════════

func resolveZpoolName(poolName string) string {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			if zn, _ := pm["zpoolName"].(string); zn != "" {
				return zn
			}
			return "nimos-" + poolName
		}
	}
	return ""
}

func detectZfsVdevType(zpoolName string) string {
	out, _ := run(fmt.Sprintf("zpool status %s 2>/dev/null", zpoolName))
	if out == "" {
		return "unknown"
	}
	if strings.Contains(out, "mirror") {
		return "mirror"
	} else if strings.Contains(out, "raidz3") {
		return "raidz3"
	} else if strings.Contains(out, "raidz2") {
		return "raidz2"
	} else if strings.Contains(out, "raidz1") || strings.Contains(out, "raidz") {
		return "raidz1"
	}
	return "stripe"
}

func getZfsPoolDisks(zpoolName string) []string {
	out, _ := run(fmt.Sprintf("zpool status %s 2>/dev/null", zpoolName))
	if out == "" {
		return []string{}
	}

	var disks []string
	inConfig := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "config:") || strings.HasPrefix(trimmed, "NAME") {
			inConfig = true
			continue
		}
		if inConfig && trimmed == "" {
			break
		}
		if inConfig {
			parts := strings.Fields(trimmed)
			if len(parts) > 0 {
				dev := parts[0]
				// Skip pool name, mirror-N, raidz-N keywords
				if dev == zpoolName || strings.HasPrefix(dev, "mirror") || strings.HasPrefix(dev, "raidz") || dev == "logs" || dev == "cache" || dev == "spares" {
					continue
				}
				if !strings.HasPrefix(dev, "/dev/") {
					dev = "/dev/" + dev
				}
				disks = append(disks, dev)
			}
		}
	}
	return disks
}

func writePoolIdentityZfs(mountPoint, name, vdevType string, disks []string) {
	identity := map[string]interface{}{
		"name":          name,
		"type":          "zfs",
		"vdevType":      vdevType,
		"disks":         disks,
		"createdAt":     time.Now().UTC().Format(time.RFC3339),
		"nimbusVersion": "5.0.0-beta",
	}
	data, _ := json.MarshalIndent(identity, "", "  ")
	os.WriteFile(filepath.Join(mountPoint, ".nimbus-pool.json"), data, 0644)
}

// ═══════════════════════════════════
// Extend getStoragePoolsGo for ZFS
// ═══════════════════════════════════

func getZfsPoolInfo(poolConf map[string]interface{}, primaryPool string) map[string]interface{} {
	poolName, _ := poolConf["name"].(string)
	zpoolName, _ := poolConf["zpoolName"].(string)
	mountPoint, _ := poolConf["mountPoint"].(string)
	vdevType, _ := poolConf["vdevType"].(string)
	createdAt, _ := poolConf["createdAt"].(string)

	if zpoolName == "" {
		zpoolName = "nimos-" + poolName
	}

	status := getZfsPoolStatus(zpoolName)
	total, _ := status["total"].(int64)
	used, _ := status["used"].(int64)
	available, _ := status["available"].(int64)
	poolStatus, _ := status["status"].(string)

	var disks []interface{}
	if d, ok := poolConf["disks"].([]interface{}); ok {
		disks = d
	} else {
		// Get from ZFS
		dList := getZfsPoolDisks(zpoolName)
		for _, d := range dList {
			disks = append(disks, d)
		}
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
		"type":               "zfs",
		"zpoolName":          zpoolName,
		"mountPoint":         mountPoint,
		"raidLevel":          vdevType,
		"vdevType":           vdevType,
		"filesystem":         "zfs",
		"createdAt":          createdAt,
		"disks":              disks,
		"status":             poolStatus,
		"health":             status["health"],
		"scrub":              status["scrub"],
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
// ZFS HTTP Routes
// ═══════════════════════════════════

func handleZfsRoutes(w http.ResponseWriter, r *http.Request, method, urlPath string, session map[string]interface{}, body map[string]interface{}) bool {
	// GET routes
	if method == "GET" {
		switch urlPath {
		case "/api/storage/zfs/importable":
			jsonOk(w, map[string]interface{}{"pools": listImportableZfsPools()})
			return true
		case "/api/storage/datasets":
			pool := r.URL.Query().Get("pool")
			zpoolName := resolveZpoolName(pool)
			if zpoolName == "" {
				jsonError(w, 400, "Pool not found")
			} else {
				jsonOk(w, map[string]interface{}{"datasets": listZfsDatasets(zpoolName)})
			}
			return true
		case "/api/storage/snapshots":
			dataset := r.URL.Query().Get("dataset")
			jsonOk(w, map[string]interface{}{"snapshots": listZfsSnapshots(dataset)})
			return true
		}
		return false
	}

	// POST routes (need admin — already checked by caller)
	if method == "POST" {
		switch urlPath {
		case "/api/storage/zfs/import":
			jsonOk(w, importPoolZfs(body))
			return true
		case "/api/storage/dataset":
			jsonOk(w, createZfsDataset(body))
			return true
		case "/api/storage/snapshot":
			jsonOk(w, createZfsSnapshot(body))
			return true
		case "/api/storage/snapshot/rollback":
			jsonOk(w, rollbackZfsSnapshot(body))
			return true
		case "/api/storage/scrub":
			jsonOk(w, startZfsScrub(body))
			return true
		}
		return false
	}

	// PUT routes
	if method == "PUT" {
		switch urlPath {
		case "/api/storage/dataset":
			jsonOk(w, updateZfsDataset(body))
			return true
		case "/api/storage/snapshots/schedule":
			conf := getStorageConfigFull()
			conf["snapshots"] = body
			saveStorageConfigFull(conf)
			jsonOk(w, map[string]interface{}{"ok": true})
			return true
		}
		return false
	}

	// DELETE routes
	if method == "DELETE" {
		switch urlPath {
		case "/api/storage/dataset":
			jsonOk(w, destroyZfsDataset(body))
			return true
		case "/api/storage/snapshot":
			jsonOk(w, destroyZfsSnapshot(body))
			return true
		}
		return false
	}

	return false
}

// ═══════════════════════════════════
// Auto-import on startup
// ═══════════════════════════════════

func zfsAutoImportOnStartup() {
	if !hasZfs {
		return
	}

	// Try to import all known pools
	run("zpool import -a -N 2>/dev/null || true")

	// Mount pools that are in config
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		poolType, _ := pm["type"].(string)
		if poolType != "zfs" {
			continue
		}
		zpoolName, _ := pm["zpoolName"].(string)
		mountPoint, _ := pm["mountPoint"].(string)
		if zpoolName == "" || mountPoint == "" {
			continue
		}

		// Check if pool is imported
		if out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName)); strings.TrimSpace(out) == "" {
			// Try to import
			run(fmt.Sprintf("zpool import %s 2>/dev/null || true", zpoolName))
		}

		// Set mount point and mount
		run(fmt.Sprintf("zfs set mountpoint=%s %s 2>/dev/null", mountPoint, zpoolName))
		run(fmt.Sprintf("zfs mount -a 2>/dev/null || true"))
	}

	logMsg("ZFS auto-import completed")
}
