package explain

import (
	"path/filepath"
	"regexp"
	"strings"
)

// pathRule maps a path pattern to a plain-language noun phrase (used in
// sentences like "打开了%s") and a short analogy.
type pathRule struct {
	re      *regexp.Regexp
	sem     string
	analogy string
}

var pathRules = []pathRule{
	{regexp.MustCompile(`^/etc/shadow$`), "系统密码库文件", "翻开了小区的住户密码登记簿"},
	{regexp.MustCompile(`^/etc/passwd$`), "系统账户清单文件", "翻看了小区的住户登记簿"},
	{regexp.MustCompile(`\.ssh/(id_rsa|id_ed25519|id_ecdsa)(\.pub)?$`), "SSH 私钥文件", "摸向了放家门钥匙的抽屉"},
	{regexp.MustCompile(`\.ssh/authorized_keys$`), "SSH 授权登录名单", "查看了谁有权凭钥匙进门的名单"},
	{regexp.MustCompile(`^/proc/\d+/mem$`), "另一个进程的内存映像文件", "直接翻看了别人脑子里装的东西"},
	{regexp.MustCompile(`^/proc/\d+/`), "某个进程的内核状态文件", "查看了某个程序的体检报告"},
	{regexp.MustCompile(`\.bash_history$|\.zsh_history$`), "命令历史记录文件", "翻看了最近敲过的那些指令"},
	{regexp.MustCompile(`\.bashrc$|\.bash_profile$|\.profile$|\.zshrc$`), "终端启动脚本", "修改了打开终端时自动念的那段开场白"},
	{regexp.MustCompile(`crontab|/etc/cron|/var/spool/cron`), "定时任务配置", "往日程表里加了一条自动提醒"},
	{regexp.MustCompile(`/etc/systemd/|/lib/systemd/|\.service$`), "系统服务配置", "改了开机自动启动的名单"},
	{regexp.MustCompile(`^/etc/`), "系统配置文件", "动了一份放在总控室里的设置单"},
	{regexp.MustCompile(`^/tmp/|^/dev/shm/|^/var/tmp/`), "临时目录下的文件", "在桌面的便签纸上写写画画，重启就没了"},
	{regexp.MustCompile(`\.so(\.\d+)*$`), "共享库文件", "借用了一本公共工具书"},
	{regexp.MustCompile(`^/dev/`), "设备文件", "直接接上了一个硬件接口"},
	{regexp.MustCompile(`\.(jpg|jpeg|png|gif|bmp|webp)$`), "图片文件", "打开了一张照片"},
	{regexp.MustCompile(`\.(mp4|mkv|mov|avi)$`), "视频文件", "打开了一段视频"},
	{regexp.MustCompile(`\.(log)$`), "日志文件", "翻看了程序自己写的流水账"},
	{regexp.MustCompile(`\.(zip|tar|gz|bz2|xz|7z|rar)$`), "压缩包文件", "打开了一个打包好的箱子"},
	{regexp.MustCompile(`\.(db|sqlite)$`), "数据库文件", "翻看了一本记录本"},
}

// pathSemantics returns a noun phrase describing path in plain language,
// plus a short everyday analogy. Falls back to a generic description with
// the basename when nothing more specific matches.
func pathSemantics(path string) (sem, analogy string) {
	if path == "" {
		return "一个文件", "翻了翻桌上的文件"
	}
	for _, r := range pathRules {
		if r.re.MatchString(path) {
			return r.sem + "（" + path + "）", r.analogy
		}
	}
	base := filepath.Base(path)
	if strings.HasPrefix(path, "/home/") || strings.Contains(path, "/.config/") {
		return "个人配置/数据文件（" + path + "）", "翻了翻自己房间里的东西"
	}
	return "文件 " + base, "翻了翻桌上的文件"
}
