package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/abxda/container-onboarding-panel/internal/fetch"
	"github.com/abxda/container-onboarding-panel/internal/podman"
)

// App es el backend Wails del launcher de contenedor.
type App struct {
	ctx          context.Context
	shuttingDown atomic.Bool
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.log("INFO", "Edición Container iniciada · v"+appVersion)
	a.log("INFO", fmt.Sprintf("Equipo: %s/%s · contenedor con Podman", runtime.GOOS, runtime.GOARCH))
	a.log("INFO", "Carpeta de trabajo: "+a.workDir())
}

func (a *App) log(level, msg string) {
	wruntime.EventsEmit(a.ctx, "log", map[string]string{"level": level, "msg": msg})
}

// beforeClose: cierre limpio homologado con las otras ediciones — si el
// contenedor está corriendo, lo detenemos antes de cerrar (no dejamos servicios
// ni puertos ocupados).
func (a *App) beforeClose(ctx context.Context) bool {
	if a.shuttingDown.Load() {
		return false
	}
	cctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if !podman.Running(cctx) {
		return false // nada que detener: cerrar directo
	}
	a.shuttingDown.Store(true)
	go func() {
		wruntime.EventsEmit(a.ctx, "shutdown:start", nil)
		sctx, c := context.WithTimeout(context.Background(), 40*time.Second)
		defer c()
		_ = podman.Stop(sctx)
		wruntime.EventsEmit(a.ctx, "shutdown:done", nil)
		wruntime.Quit(a.ctx)
	}()
	return true // prevenir cierre mientras detenemos
}

// --- tipos hacia el frontend ---

type EnvInfo struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Version      string `json:"version"`
	NeedsMachine bool   `json:"needsMachine"`
	InstallHint  string `json:"installHint"`
}

type Snapshot struct {
	PodmanOK       bool             `json:"podmanOk"`
	PodmanVersion  string           `json:"podmanVersion"`
	MachineRunning bool             `json:"machineRunning"`
	ImageLoaded    bool             `json:"imageLoaded"`
	Running        bool             `json:"running"`
	Services       podman.Services  `json:"services"`
}

type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// --- métodos enlazados ---

func (a *App) GetEnv() EnvInfo {
	_, ver := podman.Installed(a.ctx)
	return EnvInfo{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Version: ver,
		NeedsMachine: podman.NeedsMachine(),
		InstallHint:  podman.InstallHint(),
	}
}

// Diagnose devuelve el estado actual (para el dashboard y el polling).
func (a *App) Diagnose() Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, ver := podman.Installed(ctx)
	s := Snapshot{PodmanOK: ok, PodmanVersion: ver}
	if !ok {
		return s
	}
	s.MachineRunning = podman.MachineRunning(ctx)
	s.ImageLoaded = podman.HasImage(ctx)
	s.Running = podman.Running(ctx)
	if s.Running {
		s.Services = podman.CheckServices()
	}
	return s
}

// InstallPodman instala Podman con el mecanismo nativo del SO (sin asumir nada).
func (a *App) InstallPodman() Result {
	a.log("INFO", "Instalando Podman… ("+podman.InstallHint()+")")
	if runtime.GOOS == "windows" {
		a.log("WARN", "DEPENDENCIAS: en Windows, Podman corre sobre WSL2, que requiere VIRTUALIZACIÓN "+
			"por hardware. Activa: 'Subsistema de Windows para Linux (WSL2)', 'Plataforma de máquina "+
			"virtual' y 'Plataforma de hipervisor de Windows', y la virtualización (SVM/VT-x) en la BIOS.")
		a.log("WARN", "Si este equipo es una MÁQUINA VIRTUAL o no tiene virtualización, Podman NO podrá "+
			"funcionar: usa la EDICIÓN PORTABLE (no necesita WSL2 ni VirtualBox).")
	}
	ctx := context.Background()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "winget", "install", "-e", "--id", "RedHat.Podman",
			"--accept-package-agreements", "--accept-source-agreements")
	case "darwin":
		// .pkg oficial con elevación nativa (no asume Homebrew).
		script := `set u to "https://github.com/containers/podman/releases/latest/download/podman-installer-macos-universal.pkg"
do shell script "curl -fsSL -o /tmp/podman.pkg " & quoted form of u
do shell script "installer -pkg /tmp/podman.pkg -target /" with administrator privileges`
		cmd = exec.CommandContext(ctx, "osascript", "-e", script)
	default: // linux: pkexec + gestor de paquetes
		cmd = exec.CommandContext(ctx, "pkexec", "sh", "-c",
			"apt-get install -y podman || dnf install -y podman || pacman -S --noconfirm podman")
	}
	podman.Hide(cmd) // sin ventana de consola en Windows (evita el parpadeo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.log("ERROR", "No pude instalar Podman automáticamente: "+err.Error())
		if runtime.GOOS == "windows" {
			a.log("WARN", "Causa típica: falta WSL2 / virtualización (frecuente en máquinas virtuales). "+
				"Cierra este launcher y usa la EDICIÓN PORTABLE, o activa WSL2 + virtualización y reinicia. "+
				"No reintentes aquí en bucle.")
		}
		return Result{OK: false, Message: "No se pudo instalar Podman. Si tu equipo es una VM o no tiene " +
			"virtualización, usa la Edición Portable. Manual: " + podman.InstallHint()}
	}
	_ = out
	if runtime.GOOS == "windows" {
		a.log("WARN", "Podman instalado. La 1ª vez quizá debas REINICIAR Windows (para que WSL2 quede activo) "+
			"y volver a abrir el launcher. Si tras reiniciar sigue sin detectarse Podman, tu equipo probablemente "+
			"no tiene virtualización → usa la Edición Portable.")
	}
	a.log("INFO", "Podman instalado.")
	return Result{OK: true, Message: "Podman instalado."}
}

// PrepareMachine crea/arranca la podman machine (Mac/Win); en Linux es no-op.
func (a *App) PrepareMachine() Result {
	if !podman.NeedsMachine() {
		return Result{OK: true, Message: "Linux: no necesita máquina (Podman es nativo)."}
	}
	a.log("INFO", "Creando/arrancando la máquina de Podman (5500 MB)…")
	ctx := context.Background()
	if err := podman.EnsureMachine(ctx, 5500); err != nil {
		a.log("ERROR", err.Error())
		return Result{OK: false, Message: err.Error()}
	}
	a.log("INFO", "Máquina de Podman lista.")
	return Result{OK: true, Message: "Máquina lista."}
}

// DownloadImage baja la imagen precompilada de HF (con progreso) y la carga.
func (a *App) DownloadImage() Result {
	ctx := context.Background()
	e, err := fetch.ImageEntry(ctx, runtime.GOARCH)
	if err != nil {
		a.log("ERROR", err.Error())
		return Result{OK: false, Message: err.Error()}
	}
	if podman.HasImage(ctx) {
		a.log("INFO", "La imagen ya está cargada; nada que descargar.")
		return Result{OK: true, Message: "Imagen ya cargada."}
	}
	a.log("INFO", "Descargando la imagen "+e.File+" ("+e.Size+")…")
	dest := filepath.Join(os.TempDir(), e.File)
	lastPct := -1
	err = fetch.Download(ctx, e, dest, func(done, total int64) {
		pct := 0
		if total > 0 {
			pct = int(done * 100 / total)
		}
		if pct != lastPct {
			lastPct = pct
			wruntime.EventsEmit(a.ctx, "download:progress", map[string]int64{
				"pct": int64(pct), "doneMB": done / (1 << 20), "totalMB": total / (1 << 20)})
		}
	})
	if err != nil {
		a.log("ERROR", "Descarga: "+err.Error())
		return Result{OK: false, Message: err.Error()}
	}
	a.log("INFO", "Descarga OK, SHA-256 verificado. Cargando imagen…")
	if err := podman.LoadImage(ctx, dest); err != nil {
		a.log("ERROR", err.Error())
		return Result{OK: false, Message: err.Error()}
	}
	os.Remove(dest)
	a.log("INFO", "✓ Imagen "+podman.ImageTag+" cargada.")
	return Result{OK: true, Message: "Imagen lista."}
}

// StartLab arranca el contenedor del lab con el volumen de notebooks.
func (a *App) StartLab() Result {
	a.log("INFO", "Arrancando el laboratorio (contenedor)…")
	ctx := context.Background()
	if err := podman.Up(ctx, a.workDir()); err != nil {
		a.log("ERROR", err.Error())
		return Result{OK: false, Message: err.Error()}
	}
	a.log("INFO", "✓ Laboratorio arriba. Jupyter en http://localhost:8888, HDFS en :9870. (HDFS/ES tardan unos segundos.)")
	return Result{OK: true, Message: "Laboratorio arrancado."}
}

func (a *App) StopLab() Result {
	a.log("INFO", "Deteniendo el laboratorio…")
	ctx := context.Background()
	if err := podman.Stop(ctx); err != nil {
		return Result{OK: false, Message: err.Error()}
	}
	a.log("INFO", "Laboratorio detenido.")
	return Result{OK: true, Message: "Detenido."}
}

func (a *App) OpenJupyter() { wruntime.BrowserOpenURL(a.ctx, "http://localhost:8888") }
func (a *App) OpenHDFS()    { wruntime.BrowserOpenURL(a.ctx, "http://localhost:9870") }

// OpenWorkFolder abre la carpeta de trabajo (tus cuadernos) en el explorador del SO.
// Es la MISMA carpeta que se monta dentro de Jupyter como /home/quasar/work.
func (a *App) OpenWorkFolder() Result {
	dir := a.workDir()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	podman.Hide(cmd)
	_ = cmd.Start() // explorer devuelve códigos no-cero aun con éxito: no lo tratamos como error
	a.log("INFO", "Abriendo tu carpeta de trabajo: "+dir)
	return Result{OK: true, Message: dir}
}

// smokeURL es el cuaderno de prueba (TestGlobalBigData) para la variante Container (Spark 4.0).
const smokeURL = "https://huggingface.co/datasets/abxda/bdp-lab/resolve/main/cuadernos/semana_2/container/TestGlobalBigData.ipynb"

// DownloadSmokeTest baja el cuaderno de prueba a la carpeta de trabajo, listo para
// abrirlo en Jupyter (verifica HDFS + Spark + Elasticsearch de extremo a extremo).
func (a *App) DownloadSmokeTest() Result {
	dest := filepath.Join(a.workDir(), "TestGlobalBigData.ipynb")
	a.log("INFO", "Descargando el cuaderno de prueba (TestGlobalBigData)…")
	if err := downloadFile(smokeURL, dest); err != nil {
		a.log("ERROR", "No pude descargar el cuaderno: "+err.Error())
		return Result{OK: false, Message: err.Error()}
	}
	a.log("INFO", "✓ Cuaderno guardado en tu carpeta de trabajo. Ábrelo en Jupyter (carpeta work) y ejecútalo de arriba a abajo.")
	return Result{OK: true, Message: dest}
}

// downloadFile descarga una URL a un archivo local (para ficheros pequeños como cuadernos).
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d al descargar %s", resp.StatusCode, url)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// workDir es la carpeta de notebooks que se monta en el contenedor.
func (a *App) workDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	d := filepath.Join(home, "BDP-notebooks")
	_ = os.MkdirAll(d, 0o755)
	return d
}
