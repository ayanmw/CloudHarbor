package main

import (
	"fmt"

	"icode.baidu.com/baidu/xpan/go-sdk/xpan/filemanager"
)

func main() {

	// 使用示例

	// 用户的access_token
	accessToken := "your-access-token"

	// 操作类型 rename
	opera := "rename"
	// 异步
	async := "2"

	// filelist
	// 将电影下的"M020-[老Y记 1-10季][1080p]"重命名为"老Y记"
	filelist := []filemanager.FileListInfo{}
	renameFileListInfo := filemanager.FileListInfo{
		Path:    "/电影/M020-[老Y记 1-10季][1080p]",
		Newname: "老Y记",
	}
	filelist = append(filelist, renameFileListInfo)

	// // call API
	arg := filemanager.NewFileManagerArg(opera, async, filelist)
	if ret, err := filemanager.FileManager(accessToken, arg); err != nil {
		fmt.Printf("[msg: filemanager rename error] [err:%v]", err.Error())
	} else {
		fmt.Printf("ret:%+v", ret)
	}
}
