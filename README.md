# Container Onboarding Panel — Big Data Lab (3ª vía: Podman)

Launcher que lleva al alumno **de cero** a tener el laboratorio Quasar (Hadoop ·
Kafka · Elasticsearch · JupyterLab/PySpark) corriendo en un **contenedor Podman**,
desde una **imagen precompilada** que se baja de Hugging Face. Es la tercera vía
del lab, junto a **Portable** (nativo) y **Vagrant** (VM).

Homólogo al `vagrant-onboarding-panel`, con dos capas:
- **Preparación**: instalar Podman (sin asumir nada) → [crear máquina en Mac/Win]
  → descargar la imagen de HF + verificar SHA-256 → `podman load`.
- **Mi laboratorio** (idéntica a Portable/Vagrant): arrancar el contenedor,
  estado de servicios por endpoint, abrir Jupyter, start/stop, cierre limpio.

Paralelo a Vagrant: el **meta-launcher** entrega este launcher (pequeño); el
launcher baja la **imagen** (grande), igual que el panel de Vagrant baja la caja.

## Multiplataforma
- **Linux**: Podman corre los contenedores **nativo** (sin VM).
- **macOS**: Podman usa una VM ligera (`podman machine`, default Fedora CoreOS).
- **Windows**: Podman usa **WSL2** como backend de la máquina.

La **imagen** se resuelve por arquitectura (`container-image-amd64` /
`container-image-arm64` en el manifest): la amd64 sirve a Linux y a Windows/WSL2.

## Estado
- `internal/podman` — motor host-side (detect, machine, load, run, status, stop). ✅ probado.
- `internal/fetch` — manifest de HF + descarga + verificación SHA-256. ✅ probado.
- `cmd/quasarctl` — CLI de pruebas del motor: `detect | manifest | prepare | up <notebooks> | down`.
- **UI Wails** (`main.go` · `app.go` · `frontend/`) — dos capas (Preparación +
  Mi laboratorio), polling de estado, consola de registro, barra de progreso de
  descarga y cierre limpio (`beforeClose` detiene el contenedor). ✅ compila y
  corre en `windows/amd64` (detecta Podman 5.3.2 real, gating correcto).
- **Pendiente**: compilar `darwin/arm64` (agente Mac) y `linux/amd64` (agente
  Linux), publicar los launchers y activar la vía en el meta-launcher vía `.launch`.

## Publicar / actualizar
Este panel es la **vía Container** (Spark 4.0 / Scala 2.13). Se distribuye como
`bdp-container-launcher-<os>-<arch>.tar.gz` (panel chico) + la **imagen** Podman
aparte. El botón **"Cuaderno de prueba"** baja `cuadernos/semana_2/container/
TestGlobalBigData.ipynb`. Rutina completa de release (tarball+manifest anti-clobber
para el panel; imagen pesada por separado): **`Curso_BDP/PUBLICAR_Y_ACTUALIZAR.md`
§4 y §5**. Arquitectura general: `Curso_BDP/DEPLOY_MULTIOS.md`.

## Probar el motor (sin GUI)
```bash
go run ./cmd/quasarctl detect      # estado de podman/máquina/imagen/servicios
go run ./cmd/quasarctl manifest    # resuelve la imagen de tu arquitectura en HF
go run ./cmd/quasarctl prepare     # máquina + descargar imagen + verificar + load
go run ./cmd/quasarctl up ./nb     # arrancar el contenedor (Jupyter :8888, HDFS :9870)
go run ./cmd/quasarctl down        # detener
```

## Reparto del build (Wails, por plataforma)
- `darwin/arm64` → agente Mac · `linux/amd64` → agente Linux · `windows/amd64` → líder.
- Activación en el meta-launcher: automática vía `.launch` cuando se publique el launcher.

Autoría: **Dr. Abel Coronado**.
