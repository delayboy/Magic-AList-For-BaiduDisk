package mygui

import (
	"fmt"
	"github.com/Xhofe/alist/drivers/base"
	"github.com/Xhofe/alist/model"
	"github.com/Xhofe/alist/utils"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	MirrorUploadMode  = "MirrorUploadMode"
	ListMode          = "ListMode"
	DownLoadMode      = "DownLoadMode"
	UploadMode        = "UploadMode"
	Modes             = MirrorUploadMode + "," + ListMode + "," + DownLoadMode + "," + UploadMode
	DefaultMode       = ListMode
	ListFilePath      = "./listSync.txt"
	SyncCacheFilePath = "./operator.gob"
	TableFilePath     = "./table.json"
)

func (operator *SyncOperator) statisticFilesNum(nativeRootPath string) int64 {
	file := operator.GetNativeFile(nativeRootPath)
	file.Url = "/"
	totalFileNum := int64(0)
	var Queue []model.File
	Queue = append(Queue, *file)
	for {
		if len(Queue) < 1 {
			break
		}
		nowFile := Queue[0]
		nowFileNativePath := utils.Join(nativeRootPath, nowFile.Url)
		operator.accountNative.Bool1 = false
		nativeFilesMap := operator.GetNativeFilesMap(nowFileNativePath)
		for _, file := range nativeFilesMap {
			file.Url = utils.Join(nowFile.Url, file.Name)

			totalFileNum++
			if totalFileNum%5000 == 0 {
				log.Infof("%d>%s", totalFileNum, file.Url)
			}

			if file.IsDir() {
				Queue = append(Queue, file)

			}

		}
		Queue = Queue[1:] // 删除开头1个元素

	}
	log.Infof("统计完成，一共需要同步:%d个文件", totalFileNum)
	return totalFileNum
}
func (operator *SyncOperator) uploadDirToBaidu(nativeRootPath string, baiduRootPath string, file model.File) bool {

	var Queue []model.File
	Queue = append(Queue, file)
	for {
		if len(Queue) < 1 {
			break
		}
		nowFile := Queue[0]
		nowFileNativePath := utils.Join(nativeRootPath, nowFile.Url)
		nativeFilesMap := operator.GetNativeFilesMap(nowFileNativePath)
		for _, file := range nativeFilesMap {
			file.Url = utils.Join(nowFile.Url, file.Name)

			if file.IsDir() {
				if operator.createBaiduDir(baiduRootPath + file.Url) {
					Queue = append(Queue, file)
				} else {
					return false
				}

			} else {
				operator.CpToBaiduFile(nativeRootPath, baiduRootPath, file, false)
			}

		}
		Queue = Queue[1:] // 删除开头1个元素

	}
	return true

}
func (operator *SyncOperator) GetBaiduFilesMap(dirPath string) (map[string]model.File, bool) {
	dict := make(map[string]model.File)
	files, err := operator.driversBaidu.Files(dirPath, &operator.accountBaidu)
	if err == nil {
		for _, file := range files {
			dict[file.Name] = file
		}
		return dict, true
	} else {

		return dict, false
	}
}

func (operator *SyncOperator) GetNativeFile(filePath string) *model.File {
	file, _ := operator.driversNative.File(filePath, &operator.accountNative)
	return file
}
func (operator *SyncOperator) GetNativeFilesMap(dirPath string) map[string]model.File {
	dict := make(map[string]model.File)
	files, _ := operator.driversNative.Files(dirPath, &operator.accountNative)
	for _, file := range files {
		dict[file.Name] = file
	}
	return dict
}
func (operator *SyncOperator) deleteBaiduFile(filePath string) bool {
	retryNum := 0
	for {
		err := operator.driversBaidu.Delete(filePath, &operator.accountBaidu)
		if err != nil {
			log.Errorf("继续尝试(%d)文件删除失败(%s)(%s)", retryNum, filePath, err)
			retryNum++
			operator.pauseAndSaveListener(retryNum)
			time.Sleep(time.Second * 3)
		} else {
			log.Infof("文件删除成功(%s)", filePath)
			return true
		}
	}

}
func (operator *SyncOperator) createBaiduDir(dirPath string) bool {
	retryNum := 0
	for {
		err := operator.driversBaidu.MakeDir(dirPath, &operator.accountBaidu)
		if err != nil {
			log.Errorf("继续尝试(%d)文件夹创建失败(%s)", retryNum, dirPath)
			retryNum++
			operator.pauseAndSaveListener(retryNum)
			time.Sleep(time.Second)
		} else {
			log.Infof("文件夹创建成功(%s)", dirPath)
			return true
		}
	}

}
func (operator *SyncOperator) CpToBaiduFile(nativePath string, baiduPath string, file model.File, update bool) bool {

	nativeFilePath := filepath.Join(operator.accountNative.RootFolder, nativePath, file.Url)
	aimPath := utils.Join(baiduPath, file.Url)
	if update {
		if !operator.deleteBaiduFile(aimPath) {
			log.Errorf("由于删除失败，导致上传到百度网盘失败!(%s)->(%s)", nativePath, aimPath)
			return false
		}
	}
	dir, name := filepath.Split(aimPath)
	retryNum := 0
	for {
		if open, err := os.Open(nativeFilePath); err == nil {
			fileStream := model.FileStream{
				File:       open,
				Size:       uint64(file.Size),
				ParentPath: dir,
				Name:       name,
				MIMEType:   "application/octet-stream",
			}

			err = operator.driversBaidu.Upload(&fileStream, &operator.accountBaidu)
			if err != nil {
				log.Errorf("继续尝试(%d)上传到百度网盘失败!(%s)->(%s)", retryNum, nativePath, aimPath)
				retryNum += 3 //已经在Upload里Retry过了 因此直接加3
				operator.pauseAndSaveListener(retryNum)
				time.Sleep(time.Second)
			} else {
				log.Infof("已上传(%s)->(%s)", nativePath, aimPath)

				return true
			}
			_ = open.Close()
		} else {
			log.Errorf("本地文件打开失败(%s)", nativePath)
			return false
		}
	}

}

func (operator *SyncOperator) CpFromBaiduFileWithIDMan(nativePath string, baiduPath string, file model.File) {
	argument := base.Args{
		Path: file.Url,
	}
	nativeSubFile := strings.Replace(file.Url, baiduPath, "", 1) //设置硬盘上对应的子路径
	nativeSubPath := filepath.Dir(nativeSubFile)                 //去除文件名，只保留文件夹
	nativeFilePath := filepath.Join(operator.accountNative.RootFolder, nativePath, nativeSubPath)
	link, _ := operator.driversBaidu.Link(argument, &operator.accountBaidu)
	c := exec.Command("idman.exe", "/n", "/a", "/d", link.Url, "/p", nativeFilePath, "/f", file.Name)
	if err := c.Run(); err != nil {
		log.Error(err)
	}
}

func (operator *SyncOperator) GetBaiduSubFilesList(dirFile model.File) []model.File {
	var subList []model.File
	var Queue []model.File

	Queue = append(Queue, dirFile)
	for {
		if len(Queue) < 1 {
			break
		}
		nowFile := Queue[0]
		files, err := operator.driversBaidu.Files(nowFile.Url, &operator.accountBaidu)
		if err != nil {
			log.Error(err)
		}
		for _, file := range files {

			if file.IsDir() {
				Queue = append(Queue, file)
			}
			subList = append(subList, file)

		}
		Queue = Queue[1:] // 删除开头1个元素

	}
	return subList
}
func (operator *SyncOperator) CpDirOrFileFromBaiduWithIDMan(nativePath string, baiduPath string, file model.File) bool {
	if file.IsDir() {
		subList := operator.GetBaiduSubFilesList(file)
		for _, subFile := range subList {
			if !subFile.IsDir() {
				operator.CpFromBaiduFileWithIDMan(nativePath, baiduPath, subFile)
			}
		}
	} else {
		operator.CpFromBaiduFileWithIDMan(nativePath, baiduPath, file)
	}
	return true

}
func (operator *SyncOperator) baiduNoneFileCallBack(nativeRootPath string, baiduRootPath string, file model.File, baiduFile model.File, mode string) {
	action := NoAction
	if file.IsDir() {
		//创建文件夹
		operator.NewFileNum++
		listFile.WriteString(fmt.Sprintf("在网盘创建文件夹>(%s)\n", baiduRootPath+file.Url))
		if mode == MirrorUploadMode || mode == UploadMode {
			//创建文件夹并入队列
			action = CreateDir
			if operator.createBaiduDir(baiduRootPath + file.Url) {
				operator.Queue = append(operator.Queue, file)
			}
		}

	} else {
		//复制不存在的文件
		operator.NewFileNum++
		listFile.WriteString(fmt.Sprintf("上传百度网盘没有的文件>(%s)\n", nativeRootPath+file.Url))
		if mode == MirrorUploadMode || mode == UploadMode {
			//复制不存在的文件
			action = Upload
			operator.CpToBaiduFile(nativeRootPath, baiduRootPath, file, false)
		}
	}
	//根据模式的不同生成不同的action，最后统一提交到一个动作执行接口，来进行动作执行
	pk := SyncFilePk{
		BaiduNoneFile,
		nativeRootPath,
		baiduRootPath,
		file,
		baiduFile,
		action,
	}
	addToTable(pk)

}
func (operator *SyncOperator) nativeNoneFileCallBack(nativeRootPath string, baiduRootPath string, file model.File, baiduFile model.File, mode string) {
	action := NoAction
	operator.DeleteFileNum++
	listFile.WriteString(fmt.Sprintf("删除网盘上存在的文件>(%s)\n", baiduFile.Url))
	if mode == MirrorUploadMode {
		action = Delete
		operator.deleteBaiduFile(baiduFile.Url)
	} else if mode == DownLoadMode {
		action = DownLoad
		operator.CpDirOrFileFromBaiduWithIDMan(nativeRootPath, baiduRootPath, baiduFile)
	}
	pk := SyncFilePk{
		NativeNoneFile,
		nativeRootPath,
		baiduRootPath,
		file,
		baiduFile,
		action,
	}
	addToTable(pk)
}
func (operator *SyncOperator) sameSizeCallBack(nativeRootPath string, baiduRootPath string, file model.File, baiduFile model.File, mode string) {
	//log.Infof("skip(%s)", BaiduFile.Url)
	/*_, err := listFile.WriteString(fmt.Sprintf("上传百度网盘没有的文件>(%s)\n", NativeRootPath+File.Url))
	if err != nil {
		log.Error(err)
	}*/
	/*	if mode == ListMode {
		pk := SyncFilePk{
			SameSizeFile,
			NativeRootPath,
			BaiduRootPath,
			File,
			BaiduFile,
			NoAction,
		}
		addToTable(pk)
	}*/

}
func (operator *SyncOperator) diffSizeCallBack(nativeRootPath string, baiduRootPath string, file model.File, baiduFile model.File, mode string) {
	action := NoAction
	operator.UpdateFileNum++
	listFile.WriteString(fmt.Sprintf("向网盘更新不同的文件>(%s)>(%s)\n", nativeRootPath+file.Url, baiduRootPath+file.Url))
	if mode == MirrorUploadMode || mode == UploadMode {
		action = Upload
		operator.CpToBaiduFile(nativeRootPath, baiduRootPath, file, true)
	}
	pk := SyncFilePk{
		DiffSizeFile,
		nativeRootPath,
		baiduRootPath,
		file,
		baiduFile,
		action,
	}
	addToTable(pk)
}
