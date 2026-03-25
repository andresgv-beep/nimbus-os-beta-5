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

// ensurePoolsMounted se llama en startup. Garantiza que todos los pools
// registrados en storage.json estén montados, independientemente del estado
// de fstab o de zfs properties.
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
		if isMounted(mountPoint) {
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
			// ext4, xfs, mdraid (type vacío = legacy ext4/xfs)
			mounted = mountLegacyPool(pm, mountPoint)
		}

		if mounted {
			logMsg("Pool '%s' mounted successfully at %s", poolName, mountPoint)
			// Reparar fstab si falta la entrada
			repairFstabIfNeeded(pm, mountPoint, poolType)
		} else {
			logMsg("ERROR: Pool '%s' could not be mounted at %s", poolName, mountPoint)
		}
	}

	logMsg("ensurePoolsMounted completed")
}

// isMounted comprueba si un mountPoint tiene algo montado encima que no sea
// el filesystem raíz.
func isMounted(mountPoint string) bool {
	out, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if !ok || strings.TrimSpace(out) == "" {
		return false
	}
	// Descartar que sea el disco raíz (/ montado sobre el mountpoint)
	rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
	return strings.TrimSpace(out) != strings.TrimSpace(rootSrc)
}

// ── ZFS ──────────────────────────────────────────────────────────────────────

func mountZfsPool(pm map[string]interface{}, mountPoint string) bool {
	zpoolName, _ := pm["zpoolName"].(string)
	if zpoolName == "" {
		// Fallback: reconstruir zpoolName desde name
		name, _ := pm["name"].(string)
		zpoolName = "nimos-" + name
	}

	// Paso 1: ¿está importado?
	out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName))
	if strings.TrimSpace(out) == "" {
		// No importado — intentar importar por nombre
		logMsg("ZFS: importing pool %s", zpoolName)
		if _, ok := run(fmt.Sprintf("zpool import %s 2>&1", zpoolName)); !ok {
			// Último recurso: import -a (importa todos los detectables)
			run("zpool import -a 2>/dev/null || true")
			// Verificar de nuevo
			out2, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName))
			if strings.TrimSpace(out2) == "" {
				logMsg("ZFS: cannot import pool %s", zpoolName)
				return false
			}
		}
	}

	// Paso 2: Forzar mountpoint property (fix para pools importados sin -N)
	run(fmt.Sprintf("zfs set mountpoint=%s %s 2>/dev/null", mountPoint, zpoolName))

	// Paso 3: Montar — primero intentar mount explícito del pool
	if _, ok := run(fmt.Sprintf("zfs mount %s 2>&1", zpoolName)); !ok {
		// Si falla (ya montado o error), intentar mount -a
		run("zfs mount -a 2>/dev/null || true")
	}

	// Paso 4: Verificar
	return isMounted(mountPoint)
}

// ── BTRFS ─────────────────────────────────────────────────────────────────────

func mountBtrfsPool(pm map[string]interface{}, mountPoint string) bool {
	disksRaw, _ := pm["disks"].([]interface{})
	if len(disksRaw) == 0 {
		return false
	}

	// Obtener el primer disco del pool para identificar el UUID BTRFS
	firstDisk, _ := disksRaw[0].(string)
	if !strings.HasPrefix(firstDisk, "/dev/") {
		firstDisk = "/dev/" + firstDisk
	}
	// Para BTRFS con particiones, buscar la partición 1
	part := firstDisk + "1"
	if _, err := os.Stat(part); err != nil {
		part = firstDisk // sin partición, usar el disco directo
	}

	// Intentar 1: mount desde fstab (si la entrada existe)
	if _, ok := run(fmt.Sprintf("mount %s 2>/dev/null", mountPoint)); ok {
		return isMounted(mountPoint)
	}

	// Intentar 2: mount por UUID (robusto ante fstab corrupto)
	uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", part))
	uuid = strings.TrimSpace(uuid)
	if uuid != "" {
		opts := getBtrfsMountOptions(isRotationalDisk(firstDisk))
		if _, ok := run(fmt.Sprintf("mount -t btrfs -o %s UUID=%s %s 2>&1", opts, uuid, mountPoint)); ok {
			return isMounted(mountPoint)
		}
	}

	// Intentar 3: mount directo por device (último recurso)
	opts := getBtrfsMountOptions(isRotationalDisk(firstDisk))
	run(fmt.Sprintf("mount -t btrfs -o %s %s %s 2>/dev/null || true", opts, part, mountPoint))
	return isMounted(mountPoint)
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
		return isMounted(mountPoint)
	}

	// Intentar 2: para mdraid, asegurarse que el array está activo
	if arrayName != "" {
		mdPath := "/dev/" + arrayName
		// ¿existe el md device?
		if _, err := os.Stat(mdPath); err != nil {
			// Intentar reensamblar
			run(fmt.Sprintf("mdadm --assemble %s --scan 2>/dev/null || true", mdPath))
			time.Sleep(2 * time.Second)
		}
		// Mount por device
		if _, ok := run(fmt.Sprintf("mount -t %s %s %s 2>&1", filesystem, mdPath, mountPoint)); ok {
			return isMounted(mountPoint)
		}
	}

	// Intentar 3: mount por UUID buscando desde los discos configurados
	disksRaw, _ := pm["disks"].([]interface{})
	for _, d := range disksRaw {
		disk, _ := d.(string)
		if !strings.HasPrefix(disk, "/dev/") {
			disk = "/dev/" + disk
		}
		// Probar partición 1 y disco directo
		for _, dev := range []string{disk + "1", disk} {
			uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", dev))
			uuid = strings.TrimSpace(uuid)
			if uuid != "" {
				if _, ok := run(fmt.Sprintf("mount -t %s UUID=%s %s 2>&1", filesystem, uuid, mountPoint)); ok {
					return isMounted(mountPoint)
				}
			}
		}
	}

	return false
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// getBtrfsMountOptions devuelve las opciones de mount BTRFS según el tipo de disco.
func getBtrfsMountOptions(isRotational bool) string {
	opts := "compress=zstd:3,noatime,space_cache=v2"
	if isRotational {
		opts += ",autodefrag"
	} else {
		opts += ",ssd,discard=async"
	}
	return opts
}

// isRotationalDisk comprueba si un disco es rotacional (HDD) via sysfs.
func isRotationalDisk(diskPath string) bool {
	devName := strings.TrimPrefix(diskPath, "/dev/")
	// Quitar número de partición si lo hay
	devName = strings.TrimRight(devName, "0123456789")
	rotaPath := fmt.Sprintf("/sys/block/%s/queue/rotational", devName)
	data, err := os.ReadFile(rotaPath)
	if err != nil {
		return true // asumir rotacional si no se puede detectar
	}
	return strings.TrimSpace(string(data)) == "1"
}

// repairFstabIfNeeded añade la entrada fstab si no existe, usando el device
// actualmente montado (no depende de blkid en el momento de creación del pool).
func repairFstabIfNeeded(pm map[string]interface{}, mountPoint, poolType string) {
	existing, _ := os.ReadFile("/etc/fstab")
	if strings.Contains(string(existing), mountPoint) {
		return // ya está en fstab
	}

	logMsg("repairFstab: missing entry for %s, repairing...", mountPoint)

	// Obtener source del mount activo (fiable — el pool ya está montado)
	source, ok := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if !ok || strings.TrimSpace(source) == "" {
		logMsg("repairFstab: cannot get source for %s", mountPoint)
		return
	}
	source = strings.TrimSpace(source)

	// Para ZFS no escribir en fstab — ZFS gestiona sus propios mounts
	if poolType == "zfs" {
		return
	}

	// Obtener UUID del device montado
	uuid, _ := run(fmt.Sprintf("blkid -s UUID -o value %s 2>/dev/null", source))
	uuid = strings.TrimSpace(uuid)

	filesystem, _ := pm["filesystem"].(string)
	if filesystem == "" {
		// Detectar filesystem del mount activo
		fsOut, _ := run(fmt.Sprintf("findmnt -n -o FSTYPE %s 2>/dev/null", mountPoint))
		filesystem = strings.TrimSpace(fsOut)
		if filesystem == "" {
			filesystem = "ext4"
		}
	}

	var entry string
	var opts string

	switch poolType {
	case "btrfs":
		opts = getBtrfsMountOptions(true) + ",nofail"
	default:
		opts = "defaults,nofail,noatime"
	}

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
