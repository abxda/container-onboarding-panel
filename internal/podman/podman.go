// Package podman orquesta, en el HOST, el laboratorio "Quasar" como contenedor.
// Es el equivalente al paquete internal/vagrant del panel de Vagrant, pero con
// Podman en lugar de VirtualBox+Vagrant:
//
//	Vagrant:   instalar VirtualBox+Vagrant -> box add -> vagrant up
//	Container: instalar Podman [+ machine]  -> podman load (imagen de HF) -> podman run
//
// Multiplataforma:
//   - Linux  : Podman corre los contenedores NATIVO (no hay "podman machine").
//   - macOS  : Podman usa una VM ligera (podman machine, default Fedora CoreOS).
//   - Windows: Podman usa WSL2 como backend de la máquina.
//
// La capa "Mi laboratorio" (servicios, HDFS, Jupyter) es IDÉNTICA a las otras
// ediciones: el contenedor expone los mismos puertos que el box/Portable.
package podman

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ImageTag es la etiqueta de la imagen precompilada (la que trae el .tar.gz de
// HF tras `podman load`). Debe coincidir con el .image del manifest.
const ImageTag = "quasar-bigdata:1.0"

// ContainerName es el nombre fijo del contenedor del lab.
const ContainerName = "quasar"

// Ports son los puertos del stack (host:contenedor idénticos).
var Ports = []int{8888, 9870, 9000, 9200, 9092}

func bin() string { return "podman" }

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin(), args...)
	Hide(cmd) // sin ventana de consola (evita el parpadeo en la GUI)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Installed reporta si podman está disponible y su versión de cliente. Usa
// `podman --version` (solo cliente) en vez de `podman version` (que consulta el
// servidor y falla si la máquina está apagada). Salida: "podman version 5.3.2".
func Installed(ctx context.Context) (bool, string) {
	out, err := run(ctx, "--version")
	if err != nil {
		return false, ""
	}
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) == 0 {
		return false, ""
	}
	return true, f[len(f)-1] // última palabra = versión
}

// NeedsMachine: macOS y Windows ejecutan contenedores Linux dentro de una VM
// (podman machine / WSL2). Linux corre nativo y NO necesita máquina.
func NeedsMachine() bool { return runtime.GOOS != "linux" }

// MachineRunning indica si hay una podman machine encendida (mac/win). En Linux
// siempre true (no aplica).
func MachineRunning(ctx context.Context) bool {
	if !NeedsMachine() {
		return true
	}
	out, _ := run(ctx, "machine", "list", "--format", "{{.Running}}")
	return strings.Contains(strings.ToLower(out), "true")
}

// machineExists indica si ya hay una máquina creada (mac/win).
func machineExists(ctx context.Context) bool {
	out, _ := run(ctx, "machine", "list", "--format", "{{.Name}}")
	return strings.TrimSpace(out) != ""
}

// EnsureMachine crea (si falta) y arranca la podman machine con memMB de RAM.
// En Linux es no-op. memMB debe dejar ~2.5-3 GB al SO anfitrión.
func EnsureMachine(ctx context.Context, memMB int) error {
	if !NeedsMachine() {
		return nil
	}
	if !machineExists(ctx) {
		if _, err := run(ctx, "machine", "init", "--cpus", "4",
			fmt.Sprintf("--memory=%d", memMB), "--disk-size", "30"); err != nil {
			return fmt.Errorf("machine init: %w", err)
		}
	}
	if !MachineRunning(ctx) {
		if _, err := run(ctx, "machine", "start"); err != nil {
			return fmt.Errorf("machine start: %w", err)
		}
	}
	return nil
}

// LoadImage carga la imagen precompilada desde el .tar.gz (podman load
// autodetecta el gzip). Debe correr tras descargar+verificar el SHA-256.
func LoadImage(ctx context.Context, tarGzPath string) error {
	if _, err := run(ctx, "load", "-i", tarGzPath); err != nil {
		return fmt.Errorf("podman load: %w", err)
	}
	return nil
}

// HasImage indica si la imagen del lab ya está cargada.
func HasImage(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, bin(), "image", "exists", ImageTag)
	Hide(cmd)
	return cmd.Run() == nil
}

// Up arranca el contenedor del lab: 5 puertos + volumen de notebooks. El sufijo
// :Z reetiqueta para SELinux (Fedora CoreOS de la máquina mac, y Fedora/RHEL en
// Linux); es no-op donde SELinux no aplica. Idempotente: elimina uno previo.
func Up(ctx context.Context, notebooksDir string) error {
	_, _ = run(ctx, "rm", "-f", ContainerName)
	args := []string{"run", "-d", "--name", ContainerName}
	for _, p := range Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d", p, p))
	}
	if notebooksDir != "" {
		args = append(args, "-v", notebooksDir+":/home/quasar/work:Z")
	}
	args = append(args, ImageTag)
	if _, err := run(ctx, args...); err != nil {
		return fmt.Errorf("podman run: %w", err)
	}
	return nil
}

// Running indica si el contenedor del lab está en ejecución.
func Running(ctx context.Context) bool {
	out, _ := run(ctx, "ps", "--filter", "name="+ContainerName, "--format", "{{.Names}}")
	return strings.Contains(out, ContainerName)
}

// Stop detiene el contenedor del lab (cierre limpio, como apagar la VM).
func Stop(ctx context.Context) error {
	_, err := run(ctx, "stop", "-t", "20", ContainerName)
	return err
}

// Services reporta qué servicios responden, por ENDPOINT (no por jps): el
// quasar-check da falso negativo en Jupyter, así que validamos por HTTP.
type Services struct {
	HDFS    bool `json:"hdfs"`    // NameNode UI 9870
	Elastic bool `json:"elastic"` // 9200
	Jupyter bool `json:"jupyter"` // 8888
}

func httpUp(url string) bool {
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// CheckServices sondea los endpoints del lab en localhost.
func CheckServices() Services {
	return Services{
		HDFS:    httpUp("http://localhost:9870"),
		Elastic: httpUp("http://localhost:9200"),
		Jupyter: httpUp("http://localhost:8888"),
	}
}

// InstallHint devuelve el comando sugerido para instalar Podman según el SO
// (el launcher lo ejecuta con elevación nativa). En macOS es el .pkg oficial
// (no se asume Homebrew); en Linux el gestor de paquetes; en Windows winget.
func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "instalar podman-installer-macos-universal.pkg (github.com/containers/podman/releases) con: installer -pkg <ruta> -target /"
	case "windows":
		return "winget install -e --id RedHat.Podman   (requiere WSL2 habilitado)"
	default: // linux
		return "apt-get install -y podman  |  dnf install -y podman  |  pacman -S podman"
	}
}
