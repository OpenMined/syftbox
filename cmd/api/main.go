package main

// import (
// 	"context"
// 	"fmt"
// 	"io"
// 	"log/slog"
// 	"maps"
// 	"os"
// 	"os/signal"
// 	"path/filepath"
// 	"slices"
// 	"strings"
// 	"sync"
// 	"syscall"
// 	"time"

// 	"github.com/yashgorana/syftbox-go/pkg/client"
// 	"github.com/yashgorana/syftbox-go/pkg/queue"
// 	"github.com/yashgorana/syftbox-go/pkg/utils"
// )

// func main() {
// 	// Setup logger
// 	opts := &slog.HandlerOptions{
// 		Level: slog.LevelDebug,
// 	}
// 	handler := slog.NewTextHandler(os.Stdout, opts)
// 	logger := slog.New(handler)
// 	slog.SetDefault(logger)

// 	// Setup root context with signal handling
// 	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
// 	defer stop()

// 	user := "yash@openmined.org"
// 	datasiteDir := ".data/datasites"

// 	api, err := client.NewSyftAPI("http://localhost:8080")
// 	if err != nil {
// 		slog.Error("failed to create syft api", "error", err)
// 		return
// 	}

// 	// downloader
// 	downloader, err := client.NewDownloader(client.DefaultWorkers)
// 	if err != nil {
// 		slog.Error("failed to create downloader", "error", err)
// 		return
// 	}

// 	// get view
// 	resView, err := api.GetDatasiteView(ctx, &client.GetDatasiteViewInput{
// 		User: user,
// 	})
// 	if err != nil {
// 		slog.Error("failed to get datasite view", "error", err)
// 		return
// 	}

// 	// group-by-etag
// 	unique := make(map[string]string)
// 	etagMap := make(map[string][]string)
// 	keyMap := make(map[string]*client.BlobInfo)
// 	for _, k := range resView.Files {
// 		unique[k.ETag] = k.Key
// 		etagMap[k.ETag] = append(etagMap[k.ETag], k.Key)
// 		keyMap[k.Key] = &k
// 	}

// 	// get unique keys
// 	// todo filter
// 	uniqueKeys := slices.Collect(maps.Values(unique))
// 	resUrls, err := api.GetFileUrls(ctx, &client.GetFileURLInput{
// 		User: user,
// 		Keys: uniqueKeys,
// 	})
// 	if err != nil {
// 		fmt.Println(err)
// 		return
// 	}

// 	fmt.Println("Files:", len(resView.Files))
// 	fmt.Println("Unique Files:", len(unique))
// 	fmt.Println("Downloadable Files:", len(resUrls.URLs))
// 	fmt.Println("Errors:", len(resUrls.Errors))

// 	pq := queue.NewPriorityQueue[*client.Download]()

// 	// build priority queue
// 	for _, url := range resUrls.URLs {
// 		blobInfo := keyMap[url.Key]

// 		// file size + key length
// 		priority := int(blobInfo.Size) + len(blobInfo.Key)

// 		// user's datasite should be downloaded first
// 		if strings.HasPrefix(url.Key, user) {
// 			priority = 0
// 		} else if strings.Contains(url.Key, "/rpc/") {
// 			// rpc is important
// 			priority = 1
// 		}

// 		pq.Enqueue(&client.Download{
// 			URL:      url.URL,
// 			FileName: blobInfo.ETag,
// 		}, priority)
// 	}

// 	// prepare downloading routine
// 	downloadChan := make(chan *client.Download, len(resUrls.URLs))
// 	var wg sync.WaitGroup
// 	wg.Add(2)

// 	// dequeue to download channel
// 	go func() {
// 		defer wg.Done()
// 		for pq.Len() > 0 {
// 			downloadObj, _ := pq.Dequeue()
// 			downloadChan <- downloadObj
// 		}
// 		close(downloadChan)
// 		slog.Info("enqueued all files", "count", len(resUrls.URLs))
// 	}()

// 	// streaming download
// 	go func() {
// 		defer wg.Done()
// 		slog.Info("downloading files", "count", len(resUrls.URLs))
// 		workers := 8
// 		resultChan := downloader.DownloadBulkChan(ctx, downloadChan)
// 		var innerWg sync.WaitGroup

// 		start := time.Now()
// 		for range workers {
// 			innerWg.Add(1)
// 			go func() {
// 				defer innerWg.Done()
// 				for val := range resultChan {
// 					if val.Error != nil {
// 						slog.Error("failed to download file", "error", val.Error)
// 						continue
// 					}

// 					// val.FileName = ETAG. Now we expand one download to duplicate files
// 					for _, key := range etagMap[val.FileName] {
// 						targetDir := filepath.Join(datasiteDir, key)
// 						copyFile(val.DownloadPath, targetDir)
// 					}
// 				}
// 			}()
// 		}

// 		innerWg.Wait()
// 		slog.Info("downloaded all files", "took", time.Since(start))
// 	}()

// 	wg.Wait()
// 	slog.Info("cleaning up")
// 	downloader.Stop()
// 	slog.Info("Bye!")
// }

// func copyFile(src, dst string) error {
// 	if err := utils.EnsureParent(dst); err != nil {
// 		return err
// 	}

// 	// Open the source file
// 	sourceFile, err := os.Open(src)
// 	if err != nil {
// 		return err
// 	}
// 	defer sourceFile.Close()

// 	// Create the destination file
// 	destFile, err := os.Create(dst)
// 	if err != nil {
// 		return err
// 	}
// 	defer destFile.Close()

// 	// Copy the contents
// 	_, err = io.Copy(destFile, sourceFile)
// 	if err != nil {
// 		return err
// 	}

// 	// Flush contents to disk
// 	err = destFile.Sync()
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }
