package main

import (
	"fmt"

	"icode.baidu.com/baidu/xpan/go-sdk/xpan/filemanager"
)

func main() {

	// 使用示例

	// 用户的access_token
	accessToken := "your-access-token"

	// 操作类型 move
	opera := "move"
	// 异步
	async := "2"

	// filelist
	// 将我的资源下的"M020-[老Y记 1-10季][1080p]"move到电影下
	filelist := []filemanager.FileListInfo{}
	fileListInfo := filemanager.FileListInfo{
		Path:  "/我的资源/M020-[老Y记 1-10季][1080p]",
		Dest:  "/电影",
		Ondup: "fail",
	}
	filelist = append(filelist, fileListInfo)

	// // call API
	arg := filemanager.NewFileManagerArg(opera, async, filelist)
	if ret, err := filemanager.FileManager(accessToken, arg); err != nil {
		fmt.Printf("[msg: filemanager move error] [err:%v]", err.Error())
	} else {
		fmt.Printf("ret:%+v", ret)
	}

}
