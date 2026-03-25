// ============================================================================
// PATCH: Eliminar Docker de destroyPoolZfs y destroyPoolBtrfs
//
// ANTES (storage_zfs.go líneas ~207-221 y storage_btrfs.go líneas ~315-331):
//   El destroy para Docker por la fuerza, borra configs, etc.
//   Esto causa el comportamiento raro que describes.
//
// DESPUÉS:
//   El destroy NO toca Docker. Si Docker está usando el pool,
//   la UI ya habrá mostrado el warning vía /api/storage/pool/{name}/check-usage
//   y el usuario habrá parado Docker manualmente.
//   Si por algún motivo sigue activo, solo desmontamos los overlays
//   (que son submounts de Docker) pero no matamos Docker en sí.
// ============================================================================

// ── En storage_zfs.go: destroyPoolZfs ────────────────────────────────────
//
// ELIMINAR este bloque completo (líneas ~207-221):
//
//   // ── 2. Stop Docker if on this pool ──
//   dockerConf := getDockerConfigGo()
//   dockerPath, _ := dockerConf["path"].(string)
//   if dockerPath != "" && mountPoint != "" && strings.HasPrefix(dockerPath, mountPoint) {
//       run("docker stop $(docker ps -aq) 2>/dev/null || true")
//       run("docker rm $(docker ps -aq) 2>/dev/null || true")
//       run("systemctl stop docker.socket docker containerd 2>/dev/null || true")
//       run("systemctl disable docker.socket docker.service containerd.service 2>/dev/null || true")
//       run("rm -rf /var/lib/docker 2>/dev/null || true")
//       run("rm -f /etc/docker/daemon.json 2>/dev/null || true")
//       saveDockerConfigGo(map[string]interface{}{...})
//       saveInstalledApps([]map[string]interface{}{})
//       run("groupdel nimos-share-docker-apps 2>/dev/null || true")
//   }
//
// REEMPLAZAR POR:
//
//   // ── 2. Verificar que no hay contenedores activos en el pool ──
//   // (el usuario debería haber parado Docker desde la app antes de llegar aquí)
//   // Solo logueamos como aviso, no bloqueamos ni tocamos Docker.
//   if isDockerInstalledGo() && mountPoint != "" {
//       usage := checkPoolInUse(mountPoint)
//       if len(usage.Containers) > 0 {
//           logMsg("WARNING: destroying pool '%s' with active containers: %v",
//               poolName, usage.Containers)
//           // Desmontar overlays de Docker sobre este pool (sin matar Docker)
//           unmountPoolOverlays(mountPoint)
//       }
//   }

// ── En storage_btrfs.go: destroyPoolBtrfs ────────────────────────────────
//
// ELIMINAR este bloque completo (líneas ~315-331):
//
//   // ── 2. Clean Docker if on this pool ──
//   dockerConf := getDockerConfigGo()
//   dockerPath, _ := dockerConf["path"].(string)
//   if dockerPath != "" && mountPoint != "" && strings.HasPrefix(dockerPath, mountPoint) {
//       run("docker stop $(docker ps -aq) 2>/dev/null || true")
//       ... (mismo bloque que ZFS)
//   }
//
// REEMPLAZAR POR (idéntico al de ZFS):
//
//   if isDockerInstalledGo() && mountPoint != "" {
//       usage := checkPoolInUse(mountPoint)
//       if len(usage.Containers) > 0 {
//           logMsg("WARNING: destroying pool '%s' with active containers: %v",
//               poolName, usage.Containers)
//           unmountPoolOverlays(mountPoint)
//       }
//   }

// ── Función helper: unmountPoolOverlays ──────────────────────────────────
// Desmonta solo los submounts (overlays de Docker, bind mounts, etc.)
// SIN tocar el proceso Docker ni sus configuraciones.
// Va en pool_usage.go o en storage.go.

func unmountPoolOverlays(mountPoint string) {
	// Obtener todos los mounts que cuelgan de este mountPoint
	mountsOut, _ := run(fmt.Sprintf("findmnt -rn -o TARGET %s 2>/dev/null", mountPoint))
	mounts := strings.Split(strings.TrimSpace(mountsOut), "\n")

	// Desmontar en orden inverso (hijos antes que padre)
	for i := len(mounts) - 1; i >= 0; i-- {
		m := strings.TrimSpace(mounts[i])
		if m == "" || m == mountPoint {
			continue // no tocar el pool en sí
		}
		logMsg("unmountPoolOverlays: unmounting submount %s", m)
		run(fmt.Sprintf("umount -l %s 2>/dev/null || true", m))
	}
}
