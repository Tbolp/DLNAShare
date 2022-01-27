# DLNAShare
通过DLNA及ffmpeg实现文件和桌面投屏
## 环境
1 文件投屏可直接使用

2 屏幕投屏需安装ffmpeg(需要libx264)
## 参数
```
-l 搜索可用的设备
-f 文件名
-i 设备UDN字符
-n 设备名称
-w 屏幕宽度(仅linux)
-h 屏幕高度(仅linux)
-high 高画质(设备不一定支持)
```
## 示例
1 列出局域网设备
```
dlnashare -l
```
2 文件投屏
```
dlnashare -f test.mp4 -i UDN
```
3 屏幕投屏

window直接使用
```
dlnashare -i UDN
```
linux下需要设置长宽
```
dlnashare -w 1920 -h 1080 -i UDN
```
## 问题
1 windows无法搜索到设备

windows下虚拟网卡会导致发现设备失败