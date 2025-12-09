package main

import (
	"fmt"

	"icode.baidu.com/baidu/xpan/go-sdk/xpan/filemanager"
)

func main() {

	// 使用示例

	// 用户的access_token
	accessToken := "your-access-token"

	// 操作类型 delete
	opera := "delete"
	// 异步
	async := "2"

	// filelist
	// 删除我的资源下的"标准C++宝典.rar"
	filelist := []filemanager.FileListInfo{}
	deleteFileListInfo := filemanager.FileListInfo{
		Path: "/我的资源/C++程序设计语言(特别版).pdf",
	}
	filelist = append(filelist, deleteFileListInfo)

	// call API
	arg := filemanager.NewFileManagerArg(opera, async, filelist)
	if ret, err := filemanager.FileManager(accessToken, arg); err != nil {
		fmt.Printf("[msg: filemanager delete error] [err:%v]", err.Error())
	} else {
		fmt.Printf("ret:%+v", ret)
	}
}
