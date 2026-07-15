package msbimport

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// httpReadLimit caps how much of an HTTP response body we buffer into memory.
// LINE/Kakao store pages we parse here are well under 1MB in practice; a 5MB
// ceiling prevents a runaway response from exhausting RAM on the 256MB VM.
const httpReadLimit = 5 << 20

func httpDownload(link string, f string) error {
	res, err := http.Get(link)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("httpDownload: HTTP %d for %s", res.StatusCode, link)
	}
	fp, _ := os.Create(f)
	defer fp.Close()
	_, err = io.Copy(fp, res.Body)
	return err
}

// httpDownloadWithReferer downloads with a browser-like UA and Referer header.
// Some CDNs (e.g. Kakao) block bare requests without proper headers.
func httpDownloadWithReferer(link string, f string, referer string) error {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("httpDownloadWithReferer: HTTP %d for %s", res.StatusCode, link)
	}
	fp, err := os.Create(f)
	if err != nil {
		return err
	}
	defer fp.Close()
	_, err = io.Copy(fp, res.Body)
	return err
}

func httpDownloadCurlUA(link string, f string) error {
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "curl/7.61.1")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	fp, _ := os.Create(f)
	defer fp.Close()
	_, err = io.Copy(fp, res.Body)
	return err
}

func httpGet(link string) (string, error) {
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-Hant;q=0.9, ja;q=0.8, en;q=0.7")
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, httpReadLimit))
	return string(content), nil
}

// redirected link, body, error
func httpGetWithRedirLink(link string) (string, string, error) {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-Hant;q=0.9, ja;q=0.8, en;q=0.7")
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, httpReadLimit))
	return res.Request.URL.String(), string(content), nil
}

func httpGetAndroidUA(link string) (string, error) {
	req, _ := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "Android")
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, httpReadLimit))
	return string(content), nil
}

func fDownload(ctx context.Context, link string, savePath string) error {
	return fDownloadWithProgress(ctx, link, savePath, nil, nil)
}

// fDownloadWithProgress downloads link to savePath. When done/total are non-nil,
// it reports byte progress: total is fetched up-front via a HEAD request (0 if the
// CDN omits Content-Length), and done is polled from the partial file on disk so
// the caller can surface a download progress bar.
func fDownloadWithProgress(ctx context.Context, link string, savePath string, done *atomic.Int64, total *atomic.Int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, archiveDownloadTimeout)
	defer cancel()

	if total != nil {
		if size := httpContentLength(runCtx, link); size > 0 {
			total.Store(size)
		}
	}
	if done != nil {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					if fi, err := os.Stat(savePath); err == nil {
						done.Store(fi.Size())
					}
				}
			}
		}()
	}

	cmd := exec.CommandContext(runCtx, "curl",
		"--fail",
		"--location",
		"--connect-timeout", "10",
		"--max-time", fmt.Sprintf("%.0f", archiveDownloadTimeout.Seconds()),
		"--retry", "2",
		"--retry-delay", "1",
		"--output", savePath,
		link,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if runCtx.Err() != nil {
			return runCtx.Err()
		}
		return fmt.Errorf("download failed for %s: %w: %s", link, err, strings.TrimSpace(string(out)))
	}
	if done != nil {
		if fi, statErr := os.Stat(savePath); statErr == nil {
			done.Store(fi.Size())
		}
	}
	return nil
}

// httpContentLength issues a HEAD request and returns the Content-Length, or 0 if
// unknown. Failures are non-fatal — progress just falls back to byte-count only.
func httpContentLength(ctx context.Context, link string) int64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, link, nil)
	if err != nil {
		return 0
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength < 0 {
		return 0
	}
	return res.ContentLength
}

func fExtract(f string) string {
	targetDir := filepath.Join(filepath.Dir(f), SecHex(4))
	os.MkdirAll(targetDir, 0755)
	log.Debugln("Extracting to :", targetDir)

	out, err := commandOutputWithTimeout(BSDTAR_BIN, "-xvf", f, "-C", targetDir)
	if err != nil {
		log.Errorln("Error extracting:", string(out))
		return ""
	} else {
		return targetDir
	}
}

func SecHex(n int) string {
	bytes := make([]byte, n)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func ArchiveExtract(f string) []string {
	targetDir := filepath.Join(path.Dir(f), SecHex(4))
	os.MkdirAll(targetDir, 0755)
	if strings.EqualFold(filepath.Ext(f), ".zip") {
		if err := extractZIP(f, targetDir); err != nil {
			log.Warnln("ArchiveExtract ZIP error:", err)
			return []string{}
		}
		return LsFilesR(targetDir, []string{}, []string{})
	}

	out, err := commandOutputWithTimeout(BSDTAR_BIN, "-xvf", f, "-C", targetDir)
	if err != nil {
		log.Warnf("ArchiveExtract error: %v: %s", err, strings.TrimSpace(string(out)))
		return []string{}
	}
	return LsFilesR(targetDir, []string{}, []string{})
}

// extractZIP avoids bsdtar's locale-dependent filename conversion for ZIP
// archives. archive/zip preserves the entry bytes as Go strings, allowing ZIPs
// containing UTF-8 names without the language encoding flag to be extracted.
func extractZIP(archivePath, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, entry := range reader.File {
		name := filepath.Clean(entry.Name)
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe ZIP entry path: %q", entry.Name)
		}
		outputPath := filepath.Join(targetDir, name)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(outputPath, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return err
		}
		input, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, entry.Mode())
		if err == nil {
			_, err = io.Copy(output, input)
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
		input.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func LsFilesR(dir string, mustHave []string, mustNotHave []string) []string {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		accept := true
		confidence := 0
		for _, kw := range mustHave {
			if !strings.Contains(strings.ToLower(path), strings.ToLower(kw)) {
				confidence += 1
			}
		}
		if confidence < len(mustHave) {
			accept = false
		}

		for _, kw := range mustNotHave {
			if strings.Contains(strings.ToLower(path), strings.ToLower(kw)) {
				accept = false
			}
		}
		if info.IsDir() {
			accept = false
		}
		log.Debugf("accept?: %t path: %s", accept, path)
		if accept {
			files = append(files, path)
		}
		return err
	})
	log.Debugln("listed following:")
	log.Debugln(files)
	if err != nil {
		return []string{}
	} else {
		return files
	}
}

func LsFiles(dir string, mustHave []string, mustNotHave []string) []string {
	var files []string
	glob, _ := filepath.Glob(path.Join(dir, "*"))

	for _, path := range glob {
		f, _ := os.Stat(path)
		if f.IsDir() {
			continue
		}

		accept := true
		for _, kw := range mustHave {
			if !strings.Contains(strings.ToLower(path), strings.ToLower(kw)) {
				accept = false
			}
		}
		for _, kw := range mustNotHave {
			if strings.Contains(strings.ToLower(path), strings.ToLower(kw)) {
				accept = false
			}
		}
		log.Debugf("accept?: %t path: %s", accept, path)
		if accept {
			files = append(files, path)
		}
	}
	return files
}

func FCompress(f string, flist []string) error {
	// strip data dir in zip.
	// comps are 2
	comps := "2"

	args := []string{"--strip-components", comps, "-avcf", f}
	// args := []string{"-avcf", f}
	args = append(args, flist...)

	log.Debugf("Compressing strip-comps:%s to file:%s for these files:%v", comps, f, flist)
	out, err := commandOutputWithTimeout(BSDTAR_BIN, args...)
	log.Debugln(string(out))
	if err != nil {
		log.Error("Compress error!")
		log.Errorln(string(out))
	}
	return err
}

func FCompressVol(f string, flist []string) []string {
	basename := filepath.Base(f)
	dir := filepath.Dir(f)
	zipIndex := 0
	var zips [][]string
	var zipPaths []string
	var curSize int64 = 0

	for _, f := range flist {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		fSize := st.Size()
		if curSize == 0 {
			zips = append(zips, []string{})
		}
		if curSize+fSize < 50000000 {
			zips[zipIndex] = append(zips[zipIndex], f)
		} else {
			curSize = 0
			zips = append(zips, []string{})
			zipIndex += 1
			zips[zipIndex] = append(zips[zipIndex], f)
		}
		curSize += fSize
	}

	for i, files := range zips {
		var zipBN string
		if len(zips) == 1 {
			zipBN = basename
		} else {
			zipBN = strings.TrimSuffix(basename, ".zip")
			zipBN += fmt.Sprintf("_00%d.zip", i+1)
		}

		zipPath := filepath.Join(dir, zipBN)
		err := FCompress(zipPath, files)
		if err != nil {
			return nil
		}
		zipPaths = append(zipPaths, zipPath)
	}
	return zipPaths
}
