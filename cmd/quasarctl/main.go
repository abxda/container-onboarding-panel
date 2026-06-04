// quasarctl es un CLI mínimo para ejercitar y validar el motor podman del
// launcher de contenedor ANTES de envolverlo en la UI Wails. No es el producto
// final; es el banco de pruebas del paquete internal/podman.
//
//	quasarctl detect              estado de podman / máquina / imagen / servicios
//	quasarctl up   <notebooksDir> arranca el contenedor del lab
//	quasarctl down                detiene el contenedor
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/abxda/container-onboarding-panel/internal/fetch"
	"github.com/abxda/container-onboarding-panel/internal/podman"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := "detect"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "detect":
		ok, ver := podman.Installed(ctx)
		fmt.Printf("Plataforma        : %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Printf("Podman instalado  : %v %s\n", ok, ver)
		if !ok {
			fmt.Printf("  -> para instalar: %s\n", podman.InstallHint())
		}
		fmt.Printf("Necesita máquina  : %v (Linux=no; mac/win=sí)\n", podman.NeedsMachine())
		fmt.Printf("Máquina encendida : %v\n", podman.MachineRunning(ctx))
		fmt.Printf("Imagen cargada    : %v (%s)\n", podman.HasImage(ctx), podman.ImageTag)
		fmt.Printf("Contenedor activo : %v\n", podman.Running(ctx))
		s := podman.CheckServices()
		fmt.Printf("Servicios         : HDFS=%v Elasticsearch=%v Jupyter=%v\n", s.HDFS, s.Elastic, s.Jupyter)
	case "up":
		dir := ""
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		if err := podman.EnsureMachine(ctx, 6000); err != nil {
			fmt.Println("máquina:", err)
			os.Exit(1)
		}
		if err := podman.Up(ctx, dir); err != nil {
			fmt.Println("up:", err)
			os.Exit(1)
		}
		fmt.Println("contenedor lanzado; revisa http://localhost:8888 (Jupyter) y :9870 (HDFS)")
	case "down":
		if err := podman.Stop(ctx); err != nil {
			fmt.Println("stop:", err)
			os.Exit(1)
		}
		fmt.Println("contenedor detenido")
	case "manifest":
		// Resuelve la imagen para esta arquitectura SIN descargar (prueba rápida).
		e, err := fetch.ImageEntry(ctx, runtime.GOARCH)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Imagen para %s:\n  file=%s\n  sha256=%s\n  image=%s\n  size=%s\n  url=%s\n",
			runtime.GOARCH, e.File, e.SHA256, e.Image, e.Size, e.URL)
	case "prepare":
		// Flujo completo del alumno: máquina + bajar imagen + verificar + load.
		dctx := context.Background() // sin timeout: la imagen son ~GB
		e, err := fetch.ImageEntry(dctx, runtime.GOARCH)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("Preparando podman machine…")
		if err := podman.EnsureMachine(dctx, 6000); err != nil {
			fmt.Println("máquina:", err)
			os.Exit(1)
		}
		dest := os.TempDir() + string(os.PathSeparator) + e.File
		fmt.Printf("Descargando %s (%s)…\n", e.File, e.Size)
		last := -1
		if err := fetch.Download(dctx, e, dest, func(done, total int64) {
			if total > 0 {
				p := int(done * 100 / total)
				if p != last {
					last = p
					fmt.Printf("\r  %d%% (%d MB)   ", p, done/(1<<20))
				}
			}
		}); err != nil {
			fmt.Println("\ndescarga:", err)
			os.Exit(1)
		}
		fmt.Printf("\r  descarga OK, SHA verificado.            \n")
		fmt.Println("Cargando imagen (podman load)…")
		if err := podman.LoadImage(dctx, dest); err != nil {
			fmt.Println("load:", err)
			os.Exit(1)
		}
		os.Remove(dest)
		fmt.Printf("Listo. Imagen %s cargada. Usa 'quasarctl up <notebooksDir>'.\n", podman.ImageTag)
	default:
		fmt.Println("uso: quasarctl [detect|manifest|prepare|up <notebooksDir>|down]")
	}
}
