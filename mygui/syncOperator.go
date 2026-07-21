package mygui

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/Xhofe/alist/conf"
	"github.com/Xhofe/alist/drivers/base"
	"github.com/Xhofe/alist/model"
	"github.com/Xhofe/alist/utils"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var start = false
var pause = false
var listFile *os.File

// 计数器
var wg sync.WaitGroup
var syncOperator SyncOperator

// SyncOperator 如果类名首字母大写，表示其他包也能够访问
type SyncOperator struct {
	//如果说类的属性首字母大写, 表示该属性是对外能够访问的，否则的话只能够类的内部访问
	driversBaidu  base.Driver
	driversNative base.Driver
	accountBaidu  model.Account
	accountNative model.Account
	TotalFileNum  int64        `json:"TotalFileNum,omitempty"`
	FinishFileNum int64        `json:"FinishFileNum,omitempty"`
	NewFileNum    int          `json:"new_file_num,omitempty"`
	UpdateFileNum int          `json:"update_file_num,omitempty"`
	DeleteFileNum int          `json:"delete_file_num,omitempty"`
	TaskId        int          `json:"taskId" json:"task_id,omitempty"`
	Queue         []model.File `json:"queue" json:"queue,omitempty"`
}

// PathExists 判断所给路径文件/文件夹是否存在
func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	//isnotexist来判断，是不是不存在的错误
	if os.IsNotExist(err) { //如果返回的错误类型使用os.isNotExist()判断为true，说明文件或者文件夹不存在
		return false
	}
	return false
}
func deleteFile(filePath string) {
	err := os.Remove(filePath)
	if err != nil {
		log.Errorf("删除(%s)文件失败", filePath)
	}
}
func createListFile(nativeRootPath string) {
	listFilePath := ListFilePath
	var err error
	if PathExists(listFilePath) {
		listFile, err = os.OpenFile(listFilePath, os.O_APPEND, 0)
	} else {
		//新建文件
		listFile, _ = os.Create(listFilePath)

	}
	if err != nil {
		log.Fatal(err)
		return
	}

	listFile.WriteString(fmt.Sprintf("记录文件夹:%s\n", nativeRootPath))
}
func (operator *SyncOperator) syncThreadFunction() {
	nativeRootPaths := getSyncPathSetting("nativeRootPaths", []string{"Z:/Work/Notes", "Z:/Computer/Program/Program_maker"})
	baiduRootPaths := getSyncPathSetting("baiduRootPaths", []string{"/apps/FileGee文件同步备份系统/Notes", "/apps/FileGee文件同步备份系统/Program/Program_maker"})
	count := len(nativeRootPaths)
	mode := getSyncModeSetting()
	i := getSyncTaskIdSetting(count)
	if i != operator.TaskId {
		operator.Queue = nil
	}
	operator.TaskId = i
	nativeRootPath := nativeRootPaths[i]
	if nativeRootPath[1] == ':' {
		disk := nativeRootPath[0:2]
		operator.accountNative.RootFolder = disk
		nativeRootPath = nativeRootPath[2:]
	}
	operator.TotalFileNum = operator.statisticFilesNum(nativeRootPath)
	operator.listSync(nativeRootPath, baiduRootPaths[i], mode)

}
func (operator *SyncOperator) sync(nativeRootPath string, baiduRootPath string) {
	start = true
	file := operator.GetNativeFile(nativeRootPath)
	file.Url = "/"

	var queueTemp []model.File
	if operator.Queue == nil {
		operator.Queue = queueTemp
		operator.Queue = append(operator.Queue, *file)
	}

	for {
		if len(operator.Queue) < 1 {
			break
		}
		nowFile := operator.Queue[0]
		if !nowFile.IsDir() {
			log.Errorf("出现了非文件夹节点,请选择文件夹进行同步！！！")
			break
		}
		nowFileNativePath := utils.Join(nativeRootPath, nowFile.Url)
		nativeFilesMap := operator.GetNativeFilesMap(nowFileNativePath)
		nowFileAimPath := utils.Join(baiduRootPath, nowFile.Url)
		baiduFileMap, hasDir := operator.GetBaiduFilesMap(nowFileAimPath)
		operator.pauseAndSaveListener(0)

		if hasDir {
			for _, file := range nativeFilesMap {
				file.Url = utils.Join(nowFile.Url, file.Name)
				operator.FinishFileNum++
				if operator.FinishFileNum%50 == 0 {
					percent := 100.0
					percent = percent * float64(operator.FinishFileNum) / float64(operator.TotalFileNum)
					log.Infof("[%.2f%%](scan %d, new %d,update %d,delete %d)", percent, operator.FinishFileNum, operator.NewFileNum, operator.UpdateFileNum, operator.DeleteFileNum)
				}
				if baiduFile, hasFile := baiduFileMap[file.Name]; hasFile {
					operator.pauseAndSaveListener(0)
					if file.IsDir() {
						operator.Queue = append(operator.Queue, file)

					} else {
						//复制文件大小不一样的文件,使用go关键字异步执行
						if baiduFile.Size != file.Size {
							operator.CpToBaiduFile(nativeRootPath, baiduRootPath, file, true)
						} else {

							//log.Infof("skip(%s)", BaiduFile.Url)

						}

					}
				} else {
					if file.IsDir() {
						//创建文件夹并入队列
						if operator.createBaiduDir(baiduRootPath + file.Url) {
							operator.Queue = append(operator.Queue, file)
						}

					} else {
						//复制不存在的文件
						operator.CpToBaiduFile(nativeRootPath, baiduRootPath, file, false)
					}

				}

			}
			baiduFileMap, _ := operator.GetBaiduFilesMap(nowFileAimPath) //重新获取百度网盘文件
			for _, baiduFile := range baiduFileMap {
				if _, hasFile := nativeFilesMap[baiduFile.Name]; !hasFile {
					operator.deleteBaiduFile(baiduFile.Url)
				}
			}
			operator.Queue = operator.Queue[1:] // 删除开头1个元素
		} else {
			log.Errorf("致命错误，百度网盘没有找到该文件夹(%s)", nowFileAimPath)
			return

		}

	}
	start = false
	log.Infof("所有文件夹，均已同步!!!")
	operator.Queue = nil
	err := os.Remove("./operator.gob")
	if err != nil {
		log.Errorf("删除operator缓存文件失败")
	}

}

func (operator *SyncOperator) listSync(nativeRootPath string, baiduRootPath string, mode string) {
	start = true
	file := operator.GetNativeFile(nativeRootPath)
	file.Url = "/"
	createListFile(nativeRootPath)
	var queueTemp []model.File
	if operator.Queue == nil {
		operator.Queue = queueTemp
		operator.Queue = append(operator.Queue, *file)
	}
	for {
		if len(operator.Queue) < 1 {
			break
		}
		nowFile := operator.Queue[0]
		if !nowFile.IsDir() {
			log.Errorf("出现了非文件夹节点,请选择文件夹进行同步！！！")
			break
		}
		nowFileNativePath := utils.Join(nativeRootPath, nowFile.Url)
		operator.accountNative.Bool1 = true
		nativeFilesMap := operator.GetNativeFilesMap(nowFileNativePath)
		nowFileAimPath := utils.Join(baiduRootPath, nowFile.Url)
		baiduFileMap, hasDir := operator.GetBaiduFilesMap(nowFileAimPath)
		retryNum := 0
		for {
			if !hasDir {
				time.Sleep(time.Second)
				retryNum++
				baiduFileMap, hasDir = operator.GetBaiduFilesMap(nowFileAimPath)
				log.Errorf("致命错误，百度网盘没有找到该文件夹(%s),重新尝试(%d)次", nowFileAimPath, retryNum)
				operator.pauseAndSaveListener(retryNum)
			} else {
				break
			}
		}

		operator.pauseAndSaveListener(0)
		for _, file := range nativeFilesMap {
			file.Url = utils.Join(nowFile.Url, file.Name)
			operator.FinishFileNum++
			if operator.FinishFileNum%50 == 0 {
				percent := 100.0
				percent = percent * float64(operator.FinishFileNum) / float64(operator.TotalFileNum)
				precentStr := fmt.Sprintf("[%.2f%%](scan %d, new %d,update %d,delete %d)", percent, operator.FinishFileNum, operator.NewFileNum, operator.UpdateFileNum, operator.DeleteFileNum)
				GuiSetText(precentStr)
			}
			if baiduFile, hasFile := baiduFileMap[file.Name]; hasFile {
				operator.pauseAndSaveListener(0)
				if file.IsDir() {
					operator.Queue = append(operator.Queue, file)

				} else {
					// 小于5MB的文件使用md5同步，大于5MB的文件使用size同步
					needUpdate := false
					if file.Md5 != "" {
						needUpdate = file.Md5 != baiduFile.Md5
					} else {
						needUpdate = baiduFile.Size != file.Size
					}
					if needUpdate {
						operator.diffSizeCallBack(nativeRootPath, baiduRootPath, file, baiduFile, mode)
					} else {
						operator.sameSizeCallBack(nativeRootPath, baiduRootPath, file, baiduFile, mode)
					}

				}
			} else {
				operator.baiduNoneFileCallBack(nativeRootPath, baiduRootPath, file, baiduFile, mode)
			}

		}
		for _, baiduFile := range baiduFileMap {
			if nativeFile, hasFile := nativeFilesMap[baiduFile.Name]; !hasFile {
				operator.nativeNoneFileCallBack(nativeRootPath, baiduRootPath, nativeFile, baiduFile, mode)
			}
		}
		operator.Queue = operator.Queue[1:] // 删除开头1个元素

	}
	start = false
	log.Infof("所有文件夹，均已扫描!!!")
	operator.Queue = nil
	deleteFile(SyncCacheFilePath)
	listFile.Close()
	if strings.Contains(getShutDownModeSetting(), "True") {
		shutdownCmd := exec.Command("shutdown", "-s", "-t", "120")
		if err := shutdownCmd.Run(); err != nil {
			log.Error(err)
		}
	}
}
func InitSyncOperator(accounts []model.Account) SyncOperator {
	var driversBaidu base.Driver
	var driversNative base.Driver
	var accountBaidu model.Account
	var accountNative model.Account
	for i := range accounts {
		account := accounts[i]
		driver, _ := base.GetDriver(account.Type)
		log.Infof("Account:%s  Type:%s", account.Name, driver.Config().Name)
		if driver.Config().Name == "Baidu.Disk" {
			log.Infof("发现百度网盘账号：%s", account.Name)
			accountBaidu = account
			driversBaidu = driver

		}
		if driver.Config().Name == "Native" {
			log.Infof("发现本地盘账号：%s", account.Name)
			driversNative = driver
			accountNative = account
		}
	}
	operator := SyncOperator{driversBaidu, driversNative, accountBaidu, accountNative, 0, 0, 0, 0, 0, 0, nil}
	lastOperator := getOperator()
	if lastOperator != nil {
		operator.TaskId = lastOperator.TaskId
		operator.Queue = lastOperator.Queue
		operator.TotalFileNum = lastOperator.TotalFileNum
		operator.FinishFileNum = lastOperator.FinishFileNum
		operator.NewFileNum = lastOperator.NewFileNum
		operator.UpdateFileNum = lastOperator.UpdateFileNum
		operator.DeleteFileNum = lastOperator.DeleteFileNum

	} else {
		deleteFile(ListFilePath)
	}
	return operator

}
func saveSyncOperator(operator SyncOperator) {
	file, err := os.Create("./operator.gob")
	if err != nil {
		fmt.Println("文件创建失败", err.Error())
		return
	}
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(operator)
}
func getOperator() *SyncOperator {
	file, err := os.Open("./operator.gob")
	if err != nil {
		fmt.Println("文件打开失败", err.Error())
	}
	defer file.Close()
	decoder := gob.NewDecoder(file)
	var operator SyncOperator
	err = decoder.Decode(&operator)
	if err != nil {
		fmt.Println("解码失败", err.Error())
	} else {
		log.Infof("已找到找到之前缓存的文件队列，将从上一次运行结果开始")

		return &operator
	}
	return nil
}
func (operator *SyncOperator) pauseAndSaveListener(retryNum int) {
	operator.checkRetryAndPause(retryNum)
	if pause {

		saveSyncOperator(*operator)
		log.Infof("已暂停")
		wg.Add(1)
		go checkPauseThreadFunction()
		wg.Wait()
		log.Infof("已启动")
	}
}
func (operator *SyncOperator) checkRetryAndPause(retryNum int) {
	if retryNum > 10 {
		log.Errorf("已尝试超过(%d)次，无法解决，请人工查看", retryNum)
		pause = true
	}
}
func checkPauseThreadFunction() {
	for {
		if pause {
			time.Sleep(time.Second)
		} else {
			break
		}
	}
	wg.Done()
}
func serializeJson(g any) string {

	//把指针丢进去
	enc, _ := json.MarshalIndent(g, "", " ")
	//调用Encode进行序列化
	return string(enc)
}
func deSerializeJson(g string, e any) {

	//创建缓存
	buf := []byte(g)
	err := json.Unmarshal(buf, e)
	if err != nil {
		fmt.Println("解码失败", err.Error())
	}

}
func getShutDownModeSetting() string {
	setting, err := model.GetSettingByKey("ShutdownAfterTask")
	if err == nil {
		return setting.Value
	} else {

		newSetting := model.SettingItem{Key: "ShutdownAfterTask", Value: "True", Values: "True,False", Type: base.TypeSelect, Description: "whether shutdown after task over"}
		_ = model.SaveSetting(newSetting)
	}
	return "True"
}
func getSyncModeSetting() string {
	setting, err := model.GetSettingByKey("SyncMode")
	if err == nil {
		return setting.Value
	} else {

		newSetting := model.SettingItem{Key: "SyncMode", Value: DefaultMode, Values: Modes, Type: base.TypeSelect, Description: "list fake operation to File instead of really do sync"}
		_ = model.SaveSetting(newSetting)
	}
	return DefaultMode
}
func getSyncTaskIdSetting(totalTaskNum int) int {
	setting, err := model.GetSettingByKey("SyncTaskId")
	defaultValue := "0"
	if err == nil {
		res, _ := strconv.Atoi(setting.Value)
		nowTotal := strings.Count(setting.Value, ",")
		if nowTotal == totalTaskNum {
			return res
		} else {
			defaultValue = setting.Value
		}
	}
	values := ""
	for i := 0; i < totalTaskNum; i++ {
		values = values + strconv.Itoa(i) + ","
	}
	values = values + strconv.Itoa(totalTaskNum)
	newSetting := model.SettingItem{Key: "SyncTaskId", Value: defaultValue, Values: values, Type: base.TypeSelect, Description: "list fake operation to File instead of really do sync"}
	_ = model.SaveSetting(newSetting)
	res, _ := strconv.Atoi(defaultValue)
	return res
}
func getSyncPathSetting(key string, defaultPath []string) []string {
	var rootPaths []string
	setting, err := model.GetSettingByKey(key)
	if err == nil {
		rootPaths = base.StrToList(setting.Value)
	} else {
		rootPaths = defaultPath
		j := base.ListToStr(rootPaths)
		log.Infof("正在缓存设置")
		newSetting := model.SettingItem{Key: key, Value: j, Type: "text"}
		_ = model.SaveSetting(newSetting)
	}
	return rootPaths
}
func ClearCache() error {
	err := conf.Cache.Clear(conf.Ctx)
	log.Info("cache has been cleared, we will redo file sync")
	return err
}
func SyncEntry() {
	err := ClearCache()
	base.InitFileHideList()
	if err != nil || pause {
		log.Errorf("暂停")

	} else {
		log.Errorf("运行中")
	}

	if !start {
		accounts, _ := model.GetAccounts()
		syncOperator = InitSyncOperator(accounts)
		go syncOperator.syncThreadFunction()
	} else {
		pause = !pause
	}
}
