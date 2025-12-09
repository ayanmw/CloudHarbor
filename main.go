package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	log.Println("baidu-nas-downloader service starting...")

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	port := os.Getenv("NAS_DOWNLOADER_PORT")
	if port == "" {
		port = "8080"
	}

	go func() {
		for {
			// TODO: 后续在这里调用百度网盘 API，扫描 NAS_Download_Queue 并下载文件
			log.Println("[worker] tick:", time.Now().Format(time.RFC3339))
			time.Sleep(60 * time.Second)
		}
	}()

	log.Printf("HTTP server listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
