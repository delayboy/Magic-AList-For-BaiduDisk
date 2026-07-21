package native

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/Xhofe/alist/conf"
	"github.com/Xhofe/alist/drivers/base"
	"github.com/Xhofe/alist/model"
	"github.com/Xhofe/alist/utils"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Native struct{}

func (driver Native) Config() base.DriverConfig {
	return base.DriverConfig{
		Name:          "Native",
		OnlyProxy:     true,
		OnlyLocal:     true,
		NoNeedSetLink: true,
		LocalSort:     true,
	}
}

func (driver Native) Items() []base.Item {
	return []base.Item{
		{
			Name:     "root_folder",
			Label:    "root folder path",
			Type:     base.TypeString,
			Required: true,
		},
	}
}

func (driver Native) Save(account *model.Account, old *model.Account) error {
	if account == nil {
		return nil
	}
	log.Debugf("save a account: [%s]", account.Name)
	if !utils.Exists(account.RootFolder) {
		account.Status = fmt.Sprintf("[%s] not exist", account.RootFolder)
		_ = model.SaveAccount(account)
		return fmt.Errorf("[%s] not exist", account.RootFolder)
	}
	account.Status = "work"
	account.Proxy = true
	err := model.SaveAccount(account)
	if err != nil {
		return err
	}
	return nil
}

func (driver Native) File(path string, account *model.Account) (*model.File, error) {
	if utils.IsContain(strings.Split(path, "/"), "..") {
		return nil, base.ErrRelativePath
	}
	fullPath := filepath.Join(account.RootFolder, path)
	if !utils.Exists(fullPath) {
		return nil, base.ErrPathNotFound
	}
	f, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	time := f.ModTime()
	file := &model.File{
		Name:      f.Name(),
		UpdatedAt: &time,
		Driver:    driver.Config().Name,
	}
	if f.IsDir() {
		file.Type = conf.FOLDER
	} else {
		file.Type = utils.GetFileType(filepath.Ext(f.Name()))
		file.Size = f.Size()
	}
	return file, nil
}

// 百度网盘魔改MD5加密算法
// 步骤1: 字节序交换 [0:32] -> [8:16]+[0:8]+[24:32]+[16:24]
// 步骤2: XOR混淆 hex_digit ^ (15 & position_index)
// 步骤3: 位置9替换 hex数字 -> 字母 g~v
func encryptMd5(md5str string) string {
	if len(md5str) != 32 {
		return md5str
	}
	// 验证输入是否为合法的32位hex字符串
	for _, c := range md5str {
		v, err := strconv.ParseInt(string(c), 16, 64)
		if err != nil || v < 0 || v > 15 {
			return md5str
		}
	}

	// 步骤1: 字节序交换
	md5str = md5str[8:16] + md5str[0:8] + md5str[24:32] + md5str[16:24]

	// 步骤2: XOR混淆
	encryptstr := make([]byte, 32)
	for e := 0; e < 32; e++ {
		v, _ := strconv.ParseInt(string(md5str[e]), 16, 64)
		xored := v ^ int64(15&e)
		encryptstr[e] = strconv.FormatInt(xored, 16)[0]
	}

	// 步骤3: 位置9替换 hex数字 -> 字母 g~v
	digit9, _ := strconv.ParseInt(string(encryptstr[9]), 16, 64)
	encryptstr[9] = byte('g' + digit9)

	return string(encryptstr)
}

// 计算本地文件的百度加密MD5（仅对小于5MB的文件计算）
func getFileBaiduMD5(fullPath string, f model.File) string {
	if f.Size >= 5*1024*1024 {
		return ""
	}
	// 打开文件
	file, err := os.Open(filepath.Join(fullPath, f.Name))
	if err != nil {
		return ""
	}
	defer file.Close()

	// cal md5
	h1 := md5.New()

	// 小于5MB的文件直接读取全部内容计算标准MD5
	byteSize := uint64(f.Size)
	byteData := make([]byte, byteSize)

	_, err = io.ReadFull(file, byteData)
	if err != nil {
		return ""
	}
	h1.Write(byteData)
	contentMd5 := hex.EncodeToString(h1.Sum(nil))
	return encryptMd5(contentMd5)
}

func (driver Native) Files(path string, account *model.Account) ([]model.File, error) {

	if utils.IsContain(strings.Split(path, "/"), "..") {
		return nil, base.ErrRelativePath
	}
	fullPath := filepath.Join(account.RootFolder, path)
	if !utils.Exists(fullPath) {
		return nil, base.ErrPathNotFound
	}
	files := make([]model.File, 0)
	rawFiles, err := ioutil.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}
	for _, f := range rawFiles {
		//是否获取隐藏文件夹
		/*if strings.HasPrefix(f.Name(), ".") {
			continue
		}*/
		if base.IsHideFile(filepath.Join(fullPath, f.Name())) {
			continue
		}

		time := f.ModTime()
		file := model.File{
			Name:      f.Name(),
			Type:      0,
			UpdatedAt: &time,
			Driver:    driver.Config().Name,
		}
		if f.IsDir() {
			file.Type = conf.FOLDER
		} else {
			file.Type = utils.GetFileType(filepath.Ext(f.Name()))
			file.Size = f.Size()
			if account.Bool1 {
				file.Md5 = getFileBaiduMD5(fullPath, file)
			}
		}
		files = append(files, file)
	}
	_, err = base.GetCache(path, account)
	if len(files) != 0 && err != nil {
		_ = base.SetCache(path, files, account)
	}
	return files, nil
}

func (driver Native) Link(args base.Args, account *model.Account) (*base.Link, error) {
	_, err := driver.File(args.Path, account)
	if err != nil {
		return nil, err
	}
	fullPath := filepath.Join(account.RootFolder, args.Path)
	s, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	if s.IsDir() {
		return nil, base.ErrNotFile
	}
	link := base.Link{
		FilePath: fullPath,
	}
	return &link, nil
}

func (driver Native) Path(path string, account *model.Account) (*model.File, []model.File, error) {
	log.Debugf("native path: %s", path)
	file, err := driver.File(path, account)
	if err != nil {
		return nil, nil, err
	}
	if !file.IsDir() {
		//file.Url, _ = driver.Link(path, account)
		return file, nil, nil
	}
	files, err := driver.Files(path, account)
	if err != nil {
		return nil, nil, err
	}
	//model.SortFiles(files, account)
	return nil, files, nil
}

//func (driver Native) Proxy(r *http.Request, account *model.Account) {
//	// unnecessary
//}

func (driver Native) Preview(path string, account *model.Account) (interface{}, error) {
	return nil, base.ErrNotSupport
}

func (driver Native) MakeDir(path string, account *model.Account) error {
	if utils.IsContain(strings.Split(path, "/"), "..") {
		return base.ErrRelativePath
	}
	fullPath := filepath.Join(account.RootFolder, path)
	err := os.MkdirAll(fullPath, 0700)
	return err
}

func (driver Native) Move(src string, dst string, account *model.Account) error {
	if utils.IsContain(strings.Split(src+"/"+dst, "/"), "..") {
		return base.ErrRelativePath
	}
	fullSrc := filepath.Join(account.RootFolder, src)
	fullDst := filepath.Join(account.RootFolder, dst)
	return os.Rename(fullSrc, fullDst)
}

func (driver Native) Rename(src string, dst string, account *model.Account) error {
	return driver.Move(src, dst, account)
}

func (driver Native) Copy(src string, dst string, account *model.Account) error {
	if utils.IsContain(strings.Split(src+"/"+dst, "/"), "..") {
		return base.ErrRelativePath
	}
	fullSrc := filepath.Join(account.RootFolder, src)
	fullDst := filepath.Join(account.RootFolder, dst)
	srcFile, err := driver.File(src, account)
	if err != nil {
		return err
	}
	dstFile, err := driver.File(dst, account)
	if err == nil {
		if !dstFile.IsDir() {
			return base.ErrNotSupport
		}
	}
	if srcFile.IsDir() {
		return driver.CopyDir(fullSrc, fullDst)
	}
	return driver.CopyFile(fullSrc, fullDst)
}

func (driver Native) Delete(path string, account *model.Account) error {
	if utils.IsContain(strings.Split(path, "/"), "..") {
		return base.ErrRelativePath
	}
	fullPath := filepath.Join(account.RootFolder, path)
	file, err := driver.File(path, account)
	if err != nil {
		return err
	}
	if file.IsDir() {
		return os.RemoveAll(fullPath)
	}
	return os.Remove(fullPath)
}

func (driver Native) Upload(file *model.FileStream, account *model.Account) error {
	if file == nil {
		return base.ErrEmptyFile
	}
	if utils.IsContain(strings.Split(file.ParentPath, "/"), "..") {
		return base.ErrRelativePath
	}
	fullPath := filepath.Join(account.RootFolder, file.ParentPath, file.Name)
	_, err := driver.File(filepath.Join(file.ParentPath, file.Name), account)
	if err == nil {
		// TODO overwrite?
	}
	basePath := filepath.Dir(fullPath)
	if !utils.Exists(basePath) {
		err := os.MkdirAll(basePath, 0744)
		if err != nil {
			return err
		}
	}
	out, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	//var buf bytes.Buffer
	//reader := io.TeeReader(file, &buf)
	//h := md5.New()
	//_, err = io.Copy(h, reader)
	//if err != nil {
	//	return err
	//}
	//hash := hex.EncodeToString(h.Sum(nil))
	//log.Debugln("md5:", hash)
	//_, err = io.Copy(out, &buf)
	_, err = io.Copy(out, file)
	return err
}

var _ base.Driver = (*Native)(nil)
