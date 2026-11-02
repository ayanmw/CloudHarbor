# 重复文件 SHA-256 断点绑定 provider MD5

## 问题

SHA-256 断点保存累计字节和 Go hash state。若缓存条目没有 provider MD5，断点也不记录哈希开始时百度 `filemetas` 返回的 MD5。进程重启后，同 fs_id、路径、大小、mtime 不变但内容/MD5 改变时，旧前缀可能与新后缀拼成混合 SHA-256，并被登记为当前对象哈希。

## 行为要求

- 每次从 byte 0 开始时，将当前百度 filemetas 返回的 MD5 写入同一 SHA-256 checkpoint。
- 恢复时，缓存身份字段仍按原逻辑核对；checkpoint MD5 与当前 provider MD5 均存在且不一致时，走现有身份变化对账，不继续拼接字节。
- checkpoint 与当前只一方有 MD5 时，旧前缀没有可比证据，清除断点并从 byte 0 重算。
- 两者都没有 MD5 时保持既有可恢复续传行为。

## 验收

缓存对象 MD5 为空；第一轮得到 provider MD5-A 并在 16 MiB checkpoint 后取消；恢复的 provider MD5-B 必须返回身份变化而不发起后续 Range 下载。现有两边均无 MD5 的断点恢复继续通过。
