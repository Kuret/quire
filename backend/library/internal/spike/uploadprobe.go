// Quire M0.5 Q1 spike: is the xochitl USB web interface reachable from an
// on-device process, and does POST /upload work while xochitl is running?
package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const testPDF = "%PDF-1.4\n" +
	"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
	"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 405 540]/Resources<<>>>>endobj\n" +
	"trailer<</Root 1 0 R/Size 4>>\n"

func main() {
	client := &http.Client{Timeout: 20 * time.Second}

	fmt.Println("=== interface addresses ===")
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			fmt.Printf("  %-8s %s (up=%v)\n", i.Name, a.String(), i.Flags&net.FlagUp != 0)
		}
	}

	hosts := []string{"127.0.0.1", "localhost", "10.11.99.1", "[::1]"}
	if len(os.Args) > 1 {
		hosts = append(hosts, os.Args[1:]...)
	}

	fmt.Println("\n=== GET /documents/ per bind ===")
	var working string
	for _, h := range hosts {
		resp, err := client.Get("http://" + h + "/documents/")
		if err != nil {
			fmt.Printf("  %-14s FAIL  %v\n", h, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		fmt.Printf("  %-14s HTTP %d  %s\n", h, resp.StatusCode, strings.TrimSpace(string(body)))
		if resp.StatusCode == 200 && working == "" {
			working = h
		}
	}

	if working == "" {
		fmt.Println("\nVERDICT: no bind reachable from on-device. M5 must use the direct-write fallback.")
		return
	}
	fmt.Printf("\nreachable bind: %s\n", working)

	// POST /upload
	fmt.Println("\n=== POST /upload ===")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="quire-spike.pdf"`}
	h["Content-Type"] = []string{"application/pdf"}
	part, err := mw.CreatePart(h)
	if err != nil {
		fmt.Println("  part error:", err)
		return
	}
	part.Write([]byte(testPDF))
	mw.Close()

	req, _ := http.NewRequest("POST", "http://"+working+"/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://10.11.99.1")
	req.Header.Set("Referer", "http://10.11.99.1/")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("  upload FAIL:", err)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
	resp.Body.Close()
	fmt.Printf("  HTTP %d\n  body: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
}
