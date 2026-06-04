// Package fetch baja la imagen precompilada del lab desde el dataset de Hugging
// Face y verifica su SHA-256, igual que el panel de Vagrant baja la caja.
//
// La imagen se resuelve por ARQUITECTURA (no por SO): la misma imagen amd64
// sirve a Linux nativo y a Windows (WSL2); la arm64 a Apple Silicon. Clave del
// manifest: "container-image-<arch>" (container-image-amd64 / container-image-arm64).
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const ManifestURL = "https://huggingface.co/datasets/abxda/bdp-lab/resolve/main/manifest.txt"

// Entry describe la imagen a bajar.
type Entry struct {
	File   string // nombre del .tar.gz en HF
	SHA256 string // hash esperado
	Image  string // etiqueta tras `podman load` (p.ej. quasar-bigdata:1.0)
	Size   string // etiqueta informativa
	URL    string // URL absoluta de descarga (derivada del manifest)
}

func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	c := &http.Client{Timeout: 0} // sin timeout total: la imagen son ~GB
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d en %s", resp.StatusCode, url)
	}
	return resp, nil
}

// Manifest baja y parsea el manifest.txt (líneas "clave=valor"; # = comentario).
func Manifest(ctx context.Context) (map[string]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := httpGet(cctx, ManifestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			m[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
		}
	}
	return m, nil
}

// ImageEntry resuelve la imagen para una arquitectura ("amd64"/"arm64").
func ImageEntry(ctx context.Context, arch string) (Entry, error) {
	m, err := Manifest(ctx)
	if err != nil {
		return Entry{}, err
	}
	key := "container-image-" + arch
	e := Entry{
		File:   m[key+".file"],
		SHA256: m[key+".sha256"],
		Image:  m[key+".image"],
		Size:   m[key+".size"],
	}
	if e.File == "" || e.SHA256 == "" {
		return Entry{}, fmt.Errorf("no hay imagen de contenedor en el manifest para %s (clave %s)", arch, key)
	}
	base := ManifestURL[:strings.LastIndexByte(ManifestURL, '/')]
	e.URL = base + "/" + e.File
	return e, nil
}

// Download baja e.URL a dest verificando el SHA-256. onProgress recibe
// (bytesDescargados, total) para la barra (total=-1 si se desconoce).
func Download(ctx context.Context, e Entry, dest string, onProgress func(done, total int64)) error {
	resp, err := httpGet(ctx, e.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, e.SHA256) {
		os.Remove(dest)
		return fmt.Errorf("SHA-256 no coincide (esperado %s, obtenido %s)", e.SHA256, got)
	}
	return nil
}
