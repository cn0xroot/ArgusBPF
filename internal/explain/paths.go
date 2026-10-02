package explain

import (
	"path/filepath"
	"regexp"
	"strings"
)

// pathRule maps a path pattern to a plain-language noun phrase (used in
// sentences like "打开了%s" / "opened %s") and a short analogy, in both
// languages.
type pathRule struct {
	re               *regexp.Regexp
	semZh, analogyZh string
	semEn, analogyEn string
}

var pathRules = []pathRule{
	{regexp.MustCompile(`^/etc/shadow$`), "系统密码库文件", "翻开了小区的住户密码登记簿",
		"the system password vault file", "flipped open the building's password logbook"},
	{regexp.MustCompile(`^/etc/passwd$`), "系统账户清单文件", "翻看了小区的住户登记簿",
		"the system account list file", "looked through the building's resident registry"},
	{regexp.MustCompile(`\.ssh/(id_rsa|id_ed25519|id_ecdsa)(\.pub)?$`), "SSH 私钥文件", "摸向了放家门钥匙的抽屉",
		"an SSH private key file", "reached for the drawer where the house key is kept"},
	{regexp.MustCompile(`\.ssh/authorized_keys$`), "SSH 授权登录名单", "查看了谁有权凭钥匙进门的名单",
		"the SSH authorized-keys list", "checked the list of who's allowed in with a key"},
	{regexp.MustCompile(`^/proc/\d+/mem$`), "另一个进程的内存映像文件", "直接翻看了别人脑子里装的东西",
		"another process's raw memory image", "went straight through what's inside someone else's head"},
	{regexp.MustCompile(`^/proc/\d+/`), "某个进程的内核状态文件", "查看了某个程序的体检报告",
		"a process's kernel status file", "looked at a program's medical check-up report"},
	{regexp.MustCompile(`\.bash_history$|\.zsh_history$`), "命令历史记录文件", "翻看了最近敲过的那些指令",
		"the shell command history file", "flipped through the commands typed recently"},
	{regexp.MustCompile(`\.bashrc$|\.bash_profile$|\.profile$|\.zshrc$`), "终端启动脚本", "修改了打开终端时自动念的那段开场白",
		"a shell startup script", "edited the opening lines a terminal recites automatically on launch"},
	{regexp.MustCompile(`crontab|/etc/cron|/var/spool/cron`), "定时任务配置", "往日程表里加了一条自动提醒",
		"a scheduled-task (cron) config", "added an automatic reminder to the day planner"},
	{regexp.MustCompile(`/etc/systemd/|/lib/systemd/|\.service$`), "系统服务配置", "改了开机自动启动的名单",
		"a systemd service config", "edited the list of things that auto-start at boot"},
	{regexp.MustCompile(`^/etc/`), "系统配置文件", "动了一份放在总控室里的设置单",
		"a system configuration file", "touched a settings sheet kept in the control room"},
	{regexp.MustCompile(`^/tmp/|^/dev/shm/|^/var/tmp/`), "临时目录下的文件", "在桌面的便签纸上写写画画，重启就没了",
		"a file in a temporary directory", "scribbled on a sticky note on the desk — gone after a reboot"},
	{regexp.MustCompile(`\.so(\.\d+)*$`), "共享库文件", "借用了一本公共工具书",
		"a shared library file", "borrowed a shared reference book"},
	{regexp.MustCompile(`^/dev/`), "设备文件", "直接接上了一个硬件接口",
		"a device file", "plugged directly into a hardware interface"},
	{regexp.MustCompile(`\.(jpg|jpeg|png|gif|bmp|webp)$`), "图片文件", "打开了一张照片",
		"an image file", "opened a photo"},
	{regexp.MustCompile(`\.(mp4|mkv|mov|avi)$`), "视频文件", "打开了一段视频",
		"a video file", "opened a video clip"},
	{regexp.MustCompile(`\.(log)$`), "日志文件", "翻看了程序自己写的流水账",
		"a log file", "flipped through a program's own running diary"},
	{regexp.MustCompile(`\.(zip|tar|gz|bz2|xz|7z|rar)$`), "压缩包文件", "打开了一个打包好的箱子",
		"an archive file", "opened up a packed box"},
	{regexp.MustCompile(`\.(db|sqlite)$`), "数据库文件", "翻看了一本记录本",
		"a database file", "flipped through a record book"},
}

type semantics struct{ semZh, analogyZh, semEn, analogyEn string }

// pathSemantics describes path in plain language, in both languages, plus
// a short everyday analogy each. Falls back to a generic description with
// the basename when nothing more specific matches.
func pathSemantics(path string) semantics {
	if path == "" {
		return semantics{"一个文件", "翻了翻桌上的文件", "a file", "shuffled through papers on the desk"}
	}
	for _, r := range pathRules {
		if r.re.MatchString(path) {
			return semantics{r.semZh + "（" + path + "）", r.analogyZh, r.semEn + " (" + path + ")", r.analogyEn}
		}
	}
	base := filepath.Base(path)
	if strings.HasPrefix(path, "/home/") || strings.Contains(path, "/.config/") {
		return semantics{"个人配置/数据文件（" + path + "）", "翻了翻自己房间里的东西",
			"a personal config/data file (" + path + ")", "rummaged through things in their own room"}
	}
	return semantics{"文件 " + base, "翻了翻桌上的文件", "the file " + base, "shuffled through papers on the desk"}
}
