package main

import (
	"fmt"

	"icode.baidu.com/baidu/xpan/go-sdk/xpan/filemanager"
)

func main() {

	// 使用示例

	// 用户的access_token
	accessToken := "your-access-token"

	// 操作类型 copy
	opera := "copy"
	// 异步
	async := "2"

	// filelist
	// 将我的资源下的"M020-[老Y记 1-10季][1080p]"复制到电影下，并重命名为"老友记"
	filelist := []filemanager.FileListInfo{}
	fileListInfo := filemanager.FileListInfo{
		Path:    "/我的资源/M020-[老Y记 1-10季][1080p]",
		Dest:    "/电影",
		Newname: "老友记",
		Ondup:   "fail",
	}
	filelist = append(filelist, fileListInfo)

	// // call API
	arg := filemanager.NewFileManagerArg(opera, async, filelist)
	if ret, err := filemanager.FileManager(accessToken, arg); err != nil {
		fmt.Printf("[msg: filemanager copy error] [err:%v]", err.Error())
	} else {
		fmt.Printf("ret:%+v", ret)
	}

}
