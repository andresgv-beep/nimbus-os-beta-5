package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ============================================================================
// FIX: ensurePoolsMounted()
// Reemplaza la lógica de zfsAutoImportOnStartup + btrfsAutoMountOnStartup
// con una función unificada que garantiza el montaje de todos los pools.
//
// PROBLEMA ORIGINAL:
//   1. ZFS: zpool import -a -N importa sin montar. Si mountpoint property
//      no está seteado, zfs mount -a no hace nada.
//   2. BTRFS: depende de fstab. Si appendFstab falló (blkid sin UUID),
//      el pool nunca se monta en reboot.
//   3. mdraid/ext4: mismo problema que BTRFS.
//
// SOLUCIÓN:
//   - Para ZFS: import explícito por nombre + set mountpoint + mount por nombre
//   - Para BTRFS: mount por UUID desde blkid (no confiar en fstab solo)
//   - Para mdraid: mount por UUID o por device path como fallback
//   - Reparar fstab si falta la entrada (garantizar persistencia)
//   - Log claro de qué pasó con cada pool
// ============================================================================

func ensurePoolsMounted() {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		if pm == nil {
			continue
		}

		poolName, _ := pm["name"].(string)
		poolType, _ := pm["type"].(string)
		mountPoint, _ := pm["mountPoint"].(string)

		if mountPoint == "" || poolName == "" {
			continue
		}

		// Verificar si ya está montado
		if isMountedCheck(mountPoint) {
			logMsg("Pool '%s' already mounted at %s", poolName, mountPoint)
			continue
		}

		logMsg("Pool '%s' not mounted, attempting to mount...", poolName)
		os.MkdirAll(mountPoint, 0755)

		var mounted bool

		switch poolType {
		case "zfs":
			mounted = mountZfsPool(pm, mountPoint)
		case "btrfs":
			mounted = mountBtrfsPool(pm, mountPoint)
		default:
			mounted = mountLegacyPool(pm, mountPoint)
		}

		if mounted {
			logMsg("Pool '%s' mounted successfully at %s", poolName, mountPoint)
			repairFstabIfNeeded(pm, mountPoint, poolType)
		} else {
			logMsg("ERROR: Pool '%s' could not be mounted at %s", poolName, mountPoint)
			// Clean up the empty directory we created for mounting.
			// If left behind, it sits on the system disk and tricks
			// os.Stat checks into thinking the pool path exists,
			// causing writes to go to the root filesystem.
			entries, _ := os.ReadDir(mountPoint)
			if len(entries) == 0 && !isMountedCheck(mountPoint) {
				os.Remove(mountPoint)
				logMsg("Removed empty mount point %s (mount failed, protecting system disk)", mountPoint)
			}
		}
	}

	logMsg("ensurePoolsMounted completed")
}

// isMountedCheck comprueba si un mountPoint tiene algo montado que no sea root.
func isMountedCheck(mountPoint string) bool {
	out, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if !ok || strings.TrimSpace(out) == "" {
		return false
	}
	rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
	return strings.TrimSpace(out) != strings.TrimSpace(rootSrc)
}

// ── ZFS ──────────────────────────────────────────────────────────────────────

func mountZfsPool(pm map[string]interface{}, mountPoint string) bool {
	zpoolName, _ := pm["zpoolName"].(string)
	if zpoolName == "" {
		name, _ := pm["name"].(string)
		zpoolName = "nimos-" + name
	}

	// Paso 1: ¿está importado?
	out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName))
	if strings.TrimSpace(out) == "" {
		logMsg("ZFS: importing pool %s", zpoolName)
		if _, ok := run(fmt.Sprintf("zpool import %s 2>&1", zpoolName)); !ok {
			run("zpool import -a 2>/dev/null || true")
			out2, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName))
			if strings.TrimSpace(out2) == "" {
				logMsg("ZFS: cannot import pool %s", zpoolName)
				return false
			}
		}
	}

	// Paso 2: Forzar mountpoint property
	run(fmt.Sprintf("zfs set mountpoint=%s %s 2>/dev/null", mountPoint, zpoolName))

	// Paso 3: Montar
	if _, ok := run(fmt.Sprintf("zfs mount %s 2>&1", zpoolName)); !ok {
		run("zfs mount -a 2>/dev/null || true")
	}

	// Paso 4: Verificar
	return isMountedCheck(mountPoint)
}

// ── BTRFS ─────────────────────────────────────────────────────────────────────

func mountBtrfsPool(pm map[string]interface{}, mountPoint string) bool {
	disksRaw, _ := pm["disks"].([]interface{})
	if len(disksRaw) == 0 {
		return false
	}

	firstDisk, _ := disksRaw[0].(string)
	if !strings.HasPrefix(firstDisk, "/dev/") {
		firstDisk = "/dev/" + firstDisk
	}
	part := firstDisk + "1"
	if _, err := os.Stat(part); err != nil {
		part = firstDisk
	}

	// Intentar 1: mount desde fstab
	if _, ok := run(fmt.Sprintf("mount %s 2>/dev/null", mountPoint)); ok {
		if isMountedCheck(mountPoint) {
			return true
		}
	}

	// Intentar 2: mount por UUID
	uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", part))
	uuid = strings.TrimSpace(uuid)
	if uuid != "" {
		opts := btrfsMountOpts(firstDisk)
		if _, ok := run(fmt.Sprintf("mount -t btrfs -o %s UUID=%s %s 2>&1", opts, uuid, mountPoint)); ok {
			if isMountedCheck(mountPoint) {
				return true
			}
		}
	}

	// Intentar 3: mount directo por device
	opts := btrfsMountOpts(firstDisk)
	run(fmt.Sprintf("mount -t btrfs -o %s %s %s 2>/dev/null || true", opts, part, mountPoint))
	return isMountedCheck(mountPoint)
}

// ── mdraid / ext4 / xfs ───────────────────────────────────────────────────────

func mountLegacyPool(pm map[string]interface{}, mountPoint string) bool {
	filesystem, _ := pm["filesystem"].(string)
	if filesystem == "" {
		filesystem = "ext4"
	}
	arrayName, _ := pm["arrayName"].(string)

	// Intentar 1: mount desde fstab
	if _, ok := run(fmt.Sprintf("mount %s 2>/dev/null", mountPoint)); ok {
		if isMountedCheck(mountPoint) {
			return true
		}
	}

	// Intentar 2: reensamblar mdraid si aplica
	if arrayName != "" {
		mdPath := "/dev/" + arrayName
		if _, err := os.Stat(mdPath); err != nil {
			run(fmt.Sprintf("mdadm --assemble %s --scan 2>/dev/null || true", mdPath))
			time.Sleep(2 * time.Second)
		}
		if _, ok := run(fmt.Sprintf("mount -t %s %s %s 2>&1", filesystem, mdPath, mountPoint)); ok {
			if isMountedCheck(mountPoint) {
				return true
			}
		}
	}

	// Intentar 3: mount por UUID
	disksRaw, _ := pm["disks"].([]interface{})
	for _, d := range disksRaw {
		disk, _ := d.(string)
		if !strings.HasPrefix(disk, "/dev/") {
			disk = "/dev/" + disk
		}
		for _, dev := range []string{disk + "1", disk} {
			uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", dev))
			uuid = strings.TrimSpace(uuid)
			if uuid != "" {
				if _, ok := run(fmt.Sprintf("mount -t %s UUID=%s %s 2>&1", filesystem, uuid, mountPoint)); ok {
					if isMountedCheck(mountPoint) {
						return true
					}
				}
			}
		}
	}

	return false
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func btrfsMountOpts(diskPath string) string {
	opts := "compress=zstd:3,noatime,space_cache=v2"
	devName := strings.TrimPrefix(diskPath, "/dev/")
	devName = strings.TrimRight(devName, "0123456789")
	rotaPath := fmt.Sprintf("/sys/block/%s/queue/rotational", devName)
	data, err := os.ReadFile(rotaPath)
	if err == nil && strings.TrimSpace(string(data)) == "1" {
		opts += ",autodefrag"
	} else {
		opts += ",ssd,discard=async"
	}
	return opts
}

func repairFstabIfNeeded(pm map[string]interface{}, mountPoint, poolType string) {
	// ZFS gestiona sus propios mounts — no tocar fstab
	if poolType == "zfs" {
		return
	}

	existing, _ := os.ReadFile("/etc/fstab")
	if strings.Contains(string(existing), mountPoint) {
		return
	}

	logMsg("repairFstab: missing entry for %s, repairing...", mountPoint)

	source, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if !ok || strings.TrimSpace(source) == "" {
		logMsg("repairFstab: cannot get source for %s", mountPoint)
		return
	}
	source = strings.TrimSpace(source)

	uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", source))
	uuid = strings.TrimSpace(uuid)

	filesystem, _ := pm["filesystem"].(string)
	if filesystem == "" {
		fsOut, _ := run(fmt.Sprintf("findmnt -n -o FSTYPE %s 2>/dev/null", mountPoint))
		filesystem = strings.TrimSpace(fsOut)
		if filesystem == "" {
			filesystem = "ext4"
		}
	}

	var opts string
	if poolType == "btrfs" {
		opts = "compress=zstd:3,noatime,space_cache=v2,nofail"
	} else {
		opts = "defaults,nofail,noatime"
	}

	var entry string
	if uuid != "" {
		entry = fmt.Sprintf("UUID=%s %s %s %s 0 2\n", uuid, mountPoint, filesystem, opts)
	} else {
		entry = fmt.Sprintf("%s %s %s %s 0 2\n", source, mountPoint, filesystem, opts)
	}

	f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logMsg("repairFstab: cannot open /etc/fstab: %v", err)
		return
	}
	defer f.Close()
	f.WriteString(entry)
	logMsg("repairFstab: added entry for %s (UUID=%s)", mountPoint, uuid)
}
