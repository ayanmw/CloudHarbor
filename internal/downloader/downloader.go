package downloader

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

type Task struct {
	FileInfo   baiduapi.FileInfo
	Status     string // pending, downloading, done, failed
	LocalPath  string
	Error      string
	StartTime  time.Time
	FinishTime time.Time
	Progress   int64 // 已下载字节数

	// 新增字段用于断点续传和速度计算
	LastProgress     int64     // 上次记录的进度
	LastProgressTime time.Time // 上次记录进度的时间
	Speed            int64     // 当前下载速度 (bytes/sec)
	ResumeSupported  bool      // 是否支持断点续传
	MD5              string    // 文件MD5用于校验
}

// DownloadMode 下载后处理模式
// keep:  保留文件，重命名为 .可删除
// delete: 下载完成后从网盘删除
// mark_only: 仅重命名，不做其他处理（预留）
type DownloadMode string

const (
	DownloadModeKeep   DownloadMode = "keep"
	DownloadModeDelete DownloadMode = "delete"
)

// Downloader 下载器
type Downloader struct {
	cfg          *config.Config
	client       *baiduapi.Client
	tasks        map[int64]*Task // key: fs_id
	taskMutex    sync.RWMutex
	modeMutex    sync.RWMutex
	downloadMode DownloadMode
}

// SetDownloadMode 设置下载后处理模式
func (d *Downloader) SetDownloadMode(mode DownloadMode) {
	d.modeMutex.Lock()
	defer d.modeMutex.Unlock()
	d.downloadMode = mode
}

// GetDownloadMode 获取当前下载模式
func (d *Downloader) GetDownloadMode() DownloadMode {
	d.modeMutex.RLock()
	defer d.modeMutex.RUnlock()
	if d.downloadMode == "" {
		return DownloadModeKeep
	}
	return d.downloadMode
}

// NewDownloader 创建下载器
func NewDownloader(cfg *config.Config, client *baiduapi.Client) *Downloader {
	return &Downloader{
		cfg:    cfg,
		client: client,
		tasks:  make(map[int64]*Task),
	}
}

// UpdateClient 更新百度网盘客户端
func (d *Downloader) UpdateClient(client *baiduapi.Client) {
	d.client = client
}

// ScanAndDownload 扫描队列目录并下载
func (d *Downloader) ScanAndDownload() error {
	log.Printf("[Downloader] 开始扫描目录: %s", d.cfg.QueueDir)

	files, err := d.client.ListDir(d.cfg.QueueDir)
	if err != nil {
		return fmt.Errorf("列目录失败: %w", err)
	}

	log.Printf("[Downloader] 扫描到 %d 个文件/文件夹", len(files))

	for _, f := range files {
		// 跳过已标记 .可删除 的文件/文件夹
		if strings.HasSuffix(f.ServerFilename, ".可删除") {
			log.Printf("[Downloader] 跳过已标记项: %s", f.ServerFilename)
			continue
		}

		// 检查是否已在任务列表中
		d.taskMutex.RLock()
		task, exists := d.tasks[f.FsID]
		d.taskMutex.RUnlock()

		if exists && (task.Status == "done" || task.Status == "downloading") {
			log.Printf("[Downloader] 项 %s 已在任务列表 status=%s，跳过", f.ServerFilename, task.Status)
			continue
		}

		// 如果是文件夹，递归下载
		if f.IsDir == 1 {
			log.Printf("[Downloader] 发现文件夹: %s，开始递归下载", f.ServerFilename)
			go d.downloadFolder(f)
		} else {
			// 创建文件下载任务
			log.Printf("[Downloader] 创建下载任务: %s (size=%d)", f.ServerFilename, f.Size)
			go d.downloadFile(f)
		}
	}

	return nil
}

// downloadFolder 递归下载文件夹
func (d *Downloader) downloadFolder(folderInfo baiduapi.FileInfo) {
	task := &Task{
		FileInfo:  folderInfo,
		Status:    "pending",
		StartTime: time.Now(),
	}

	d.taskMutex.Lock()
	d.tasks[folderInfo.FsID] = task
	d.taskMutex.Unlock()

	d.updateTaskStatus(folderInfo.FsID, "downloading", "")

	// 创建本地文件夹
	localFolderPath := filepath.Join(d.cfg.LocalDownDir, folderInfo.ServerFilename)
	if err := os.MkdirAll(localFolderPath, 0755); err != nil {
		d.updateTaskStatus(folderInfo.FsID, "failed", fmt.Sprintf("创建本地文件夹失败: %v", err))
		return
	}
	task.LocalPath = localFolderPath

	log.Printf("[Downloader] 列出文件夹内容: %s", folderInfo.Path)

	// 列出文件夹内的所有文件
	subFiles, err := d.client.ListDir(folderInfo.Path)
	if err != nil {
		d.updateTaskStatus(folderInfo.FsID, "failed", fmt.Sprintf("列出文件夹内容失败: %v", err))
		return
	}

	log.Printf("[Downloader] 文件夹 %s 包含 %d 个项", folderInfo.ServerFilename, len(subFiles))

	// 递归下载文件夹内的所有内容
	for _, subFile := range subFiles {
		if strings.HasSuffix(subFile.ServerFilename, ".可删除") {
			continue
		}

		if subFile.IsDir == 1 {
			// 递归下载子文件夹
			subFolderInfo := subFile
			subFolderInfo.ServerFilename = filepath.Join(folderInfo.ServerFilename, subFile.ServerFilename)
			d.downloadFolder(subFolderInfo)
		} else {
			// 下载文件到对应的子目录
			subFileInfo := subFile
			subFileInfo.ServerFilename = filepath.Join(folderInfo.ServerFilename, subFile.ServerFilename)
			d.downloadFileToPath(subFileInfo, localFolderPath)
		}
	}

	// 文件夹下载完成后，重命名
	log.Printf("[Downloader] 文件夹 %s 下载完成，重命名", folderInfo.ServerFilename)
	newName := folderInfo.ServerFilename + ".可删除"
	if err := d.client.RenameFile(folderInfo.Path, newName); err != nil {
		log.Printf("[Downloader] 重命名文件夹失败（不影响下载）: %v", err)
	}

	d.updateTaskStatus(folderInfo.FsID, "done", "")
}

// downloadFileToPath 下载文件到指定本地目录
func (d *Downloader) downloadFileToPath(fileInfo baiduapi.FileInfo, localDir string) {
	task := &Task{
		FileInfo:         fileInfo,
		Status:           "pending",
		StartTime:        time.Now(),
		LastProgressTime: time.Now(),
		Speed:            0,
		ResumeSupported:  true,         // 默认支持断点续传
		MD5:              fileInfo.MD5, // 从文件信息中获取MD5
	}

	d.taskMutex.Lock()
	d.tasks[fileInfo.FsID] = task
	d.taskMutex.Unlock()

	d.updateTaskStatus(fileInfo.FsID, "downloading", "")

	// 本地文件路径（使用传入的目录）
	localPath := filepath.Join(localDir, filepath.Base(fileInfo.ServerFilename))
	task.LocalPath = localPath

	// 检查本地文件是否存在
	var startOffset int64 = 0
	if stat, err := os.Stat(localPath); err == nil {
		// 文件已存在，检查是否已完成或需要断点续传
		existingSize := stat.Size()

		// 如果文件大小已经等于目标大小，认为已完成
		if existingSize == fileInfo.Size {
			log.Printf("[Downloader] 文件已存在且大小匹配，跳过下载: %s", localPath)
			d.updateTaskStatus(fileInfo.FsID, "done", "")
			return
		}

		// 如果文件存在但大小不匹配，尝试断点续传
		startOffset = existingSize
		task.Progress = existingSize
		task.LastProgress = existingSize
		log.Printf("[Downloader] 文件已存在，尝试断点续传: %s (已下载: %d/%d)", localPath, existingSize, fileInfo.Size)
	}

	// 获取下载链接
	log.Printf("[Downloader] 获取下载链接: %s (fsid=%d)", fileInfo.Path, fileInfo.FsID)
	dlink, err := d.client.GetDownloadLink(fileInfo.FsID)
	if err != nil {
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("获取下载链接失败: %v", err))
		return
	}

	// 下载文件（支持断点续传）
	log.Printf("[Downloader] 开始下载: %s -> %s (偏移量: %d)", fileInfo.Path, localPath, startOffset)
	rc, contentLen, err := d.client.DownloadFileWithRange(dlink, startOffset)
	if err != nil {
		log.Printf("[Downloader] 下载文件失败: %v", err)
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("下载文件失败: %v", err))
		return
	}
	defer rc.Close()

	log.Printf("[Downloader] 下载流已获取，预期大小: %d 字节，开始写入文件", contentLen)

	// 打开或创建本地文件
	var outFile *os.File
	if startOffset > 0 {
		// 断点续传模式，追加写入
		outFile, err = os.OpenFile(localPath, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		// 全新下载，创建文件
		outFile, err = os.Create(localPath)
	}
	if err != nil {
		log.Printf("[Downloader] 创建/打开本地文件失败: %v", err)
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("创建/打开本地文件失败: %v", err))
		return
	}
	defer outFile.Close()

	log.Printf("[Downloader] 本地文件已打开，开始复制数据")

	// 带进度和速度计算的复制
	buf := make([]byte, 32*1024)
	var written int64 = startOffset
	lastSpeedCalcTime := time.Now()
	lastWritten := startOffset

	for {
		nr, er := rc.Read(buf)
		if nr > 0 {
			nw, ew := outFile.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				d.taskMutex.Lock()
				task.Progress = written

				// 每隔1秒计算一次速度
				if time.Since(lastSpeedCalcTime) >= time.Second {
					elapsed := time.Since(lastSpeedCalcTime).Seconds()
					if elapsed > 0 {
						task.Speed = int64(float64(written-lastWritten) / elapsed)
					}
					lastSpeedCalcTime = time.Now()
					lastWritten = written
				}

				d.taskMutex.Unlock()
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}

	if err != nil {
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("写入文件失败: %v", err))
		return
	}

	log.Printf("[Downloader] 下载完成: %s (size=%d/%d)", fileInfo.Path, written, fileInfo.Size)

	// 只有在下载完全成功的情况下才根据模式处理网盘中的文件
	if written == fileInfo.Size || (fileInfo.Size == 0 && written == 0) {
		// 对于文件夹内的文件，我们只重命名而不删除
		log.Printf("[Downloader] 重命名网盘文件: %s", fileInfo.Path)
		newName := fileInfo.ServerFilename + ".可删除"
		if err := d.client.RenameFile(fileInfo.Path, newName); err != nil {
			log.Printf("[Downloader] 重命名失败（不影响下载）: %v", err)
		}
	} else {
		log.Printf("[Downloader] 下载不完整，跳过网盘文件处理: %s (已下载: %d, 总大小: %d)", fileInfo.Path, written, fileInfo.Size)
	}

	d.updateTaskStatus(fileInfo.FsID, "done", "")
}

// downloadFile 下载单个文件
func (d *Downloader) downloadFile(fileInfo baiduapi.FileInfo) {
	task := &Task{
		FileInfo:         fileInfo,
		Status:           "pending",
		StartTime:        time.Now(),
		LastProgressTime: time.Now(),
		Speed:            0,
		ResumeSupported:  true,         // 默认支持断点续传
		MD5:              fileInfo.MD5, // 从文件信息中获取MD5
	}

	d.taskMutex.Lock()
	d.tasks[fileInfo.FsID] = task
	d.taskMutex.Unlock()

	// 更新状态为 downloading
	d.updateTaskStatus(fileInfo.FsID, "downloading", "")

	// 确保本地下载目录存在
	if err := os.MkdirAll(d.cfg.LocalDownDir, 0755); err != nil {
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("创建本地目录失败: %v", err))
		return
	}

	localPath := filepath.Join(d.cfg.LocalDownDir, fileInfo.ServerFilename)
	task.LocalPath = localPath

	// 检查本地文件是否存在
	var startOffset int64 = 0
	if stat, err := os.Stat(localPath); err == nil {
		// 文件已存在，检查是否已完成或需要断点续传
		existingSize := stat.Size()

		// 如果文件大小已经等于目标大小，认为已完成
		if existingSize == fileInfo.Size {
			log.Printf("[Downloader] 文件已存在且大小匹配，跳过下载: %s", localPath)
			d.updateTaskStatus(fileInfo.FsID, "done", "")
			return
		}

		// 如果文件存在但大小不匹配，尝试断点续传
		startOffset = existingSize
		task.Progress = existingSize
		task.LastProgress = existingSize
		log.Printf("[Downloader] 文件已存在，尝试断点续传: %s (已下载: %d/%d)", localPath, existingSize, fileInfo.Size)
	}

	// 获取下载链接
	log.Printf("[Downloader] 获取下载链接: %s (fsid=%d)", fileInfo.ServerFilename, fileInfo.FsID)
	dlink, err := d.client.GetDownloadLink(fileInfo.FsID)
	if err != nil {
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("获取下载链接失败: %v", err))
		return
	}

	// 下载文件（支持断点续传）
	log.Printf("[Downloader] 开始下载: %s -> %s (偏移量: %d)", fileInfo.ServerFilename, localPath, startOffset)
	rc, contentLen, err := d.client.DownloadFileWithRange(dlink, startOffset)
	if err != nil {
		log.Printf("[Downloader] 下载文件失败: %v", err)
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("下载文件失败: %v", err))
		return
	}
	defer rc.Close()

	log.Printf("[Downloader] 下载流已获取，预期大小: %d 字节", contentLen)

	// 打开或创建本地文件
	var outFile *os.File
	if startOffset > 0 {
		// 断点续传模式，追加写入
		outFile, err = os.OpenFile(localPath, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		// 全新下载，创建文件
		outFile, err = os.Create(localPath)
	}
	if err != nil {
		log.Printf("[Downloader] 创建/打开本地文件失败: %v", err)
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("创建/打开本地文件失败: %v", err))
		return
	}
	defer outFile.Close()

	log.Printf("[Downloader] 本地文件已打开，开始复制数据")

	// 带进度和速度计算的复制
	buf := make([]byte, 32*1024)
	var written int64 = startOffset
	lastSpeedCalcTime := time.Now()
	lastWritten := startOffset

	for {
		nr, er := rc.Read(buf)
		if nr > 0 {
			nw, ew := outFile.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				d.taskMutex.Lock()
				task.Progress = written

				// 每隔1秒计算一次速度
				if time.Since(lastSpeedCalcTime) >= time.Second {
					elapsed := time.Since(lastSpeedCalcTime).Seconds()
					if elapsed > 0 {
						task.Speed = int64(float64(written-lastWritten) / elapsed)
					}
					lastSpeedCalcTime = time.Now()
					lastWritten = written
				}

				d.taskMutex.Unlock()
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}

	if err != nil {
		log.Printf("[Downloader] 写入文件失败: %v，已写入 %d 字节", err, written)
		d.updateTaskStatus(fileInfo.FsID, "failed", fmt.Sprintf("写入文件失败: %v", err))
		return
	}

	log.Printf("[Downloader] 下载完成: %s (size=%d/%d)", fileInfo.ServerFilename, written, fileInfo.Size)

	// 只有在下载完全成功的情况下才根据模式处理网盘中的文件
	if written == fileInfo.Size || (fileInfo.Size == 0 && written == 0) {
		// 根据下载模式处理网盘中的文件
		mode := d.GetDownloadMode()
		switch mode {
		case DownloadModeDelete:
			log.Printf("[Downloader] 模式=delete，删除网盘文件: %s", fileInfo.Path)
			if err := d.client.DeleteFile(fileInfo.Path); err != nil {
				log.Printf("[Downloader] 删除网盘文件失败（不影响本地下载）: %v", err)
			}
		case DownloadModeKeep:
			fallthrough
		default:
			log.Printf("[Downloader] 模式=keep，重命名网盘文件: %s", fileInfo.ServerFilename)
			newName := fileInfo.ServerFilename + ".可删除"
			if err := d.client.RenameFile(fileInfo.Path, newName); err != nil {
				log.Printf("[Downloader] 重命名失败（不影响下载）: %v", err)
			}
		}
	} else {
		log.Printf("[Downloader] 下载不完整，跳过网盘文件处理: %s (已下载: %d, 总大小: %d)", fileInfo.ServerFilename, written, fileInfo.Size)
	}

	d.updateTaskStatus(fileInfo.FsID, "done", "")
}

// updateTaskStatus 更新任务状态
func (d *Downloader) updateTaskStatus(fsID int64, status, errMsg string) {
	d.taskMutex.Lock()
	defer d.taskMutex.Unlock()

	if task, ok := d.tasks[fsID]; ok {
		task.Status = status
		task.Error = errMsg
		if status == "done" || status == "failed" {
			task.FinishTime = time.Now()
		}
	}
}

// GetTasks 获取任务列表
func (d *Downloader) GetTasks() []*Task {
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()

	tasks := make([]*Task, 0, len(d.tasks))
	for _, t := range d.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// GetTaskByFsID 根据 fs_id 获取任务
func (d *Downloader) GetTaskByFsID(fsID int64) *Task {
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	return d.tasks[fsID]
}
