package gate

import (
	"net/http"
	"strconv"
	"strings"
)

const languageCookieName = "filegate_language"

// Both dictionaries are compiled into the executable. Keys are shared by the
// HTML template and application errors; filenames and file contents are untouched.
var translations = map[string]map[string]string{
	"zh-CN": {
		"Title": "文件访问", "Heading": "访问文件",
		"Instruction": "请输入管理员提供的一次性访问码。", "Code": "访问码",
		"Example": "例如 ab12345678", "Enter": "进入目录", "Directory": "文件目录",
		"Logout": "退出", "Parent": "上级目录", "Bytes": "字节", "Download": "下载",
		"Empty": "目录为空", "Language": "语言", "Automatic": "跟随浏览器",
		"InvalidSource": "无效来源", "Unavailable": "服务暂不可用",
		"Blocked": "访问已被禁止", "InvalidOrigin": "无效请求来源",
		"TooManyRequests": "请求过多，请稍后重试",
		"RateBanned":      "重试频率过高，IP 已临时封禁",
		"FailureBanned":   "输入错误次数过多，IP 已临时封禁",
		"InvalidCode":     "访问码无效、已使用或已过期", "SessionLimit": "会话数量已达上限",
		"SaveFailed": "无法保存登录状态，请稍后重试", "InvalidLogout": "无效退出请求",
		"NotFound": "文件或页面不存在", "ReadFailed": "无法读取文件",
		"Busy": "服务繁忙，请稍后重试", "InvalidRange": "仅支持单个且不超过 128 字节的文件范围请求",
		"DirectoryTooLarge": "目录条目过多，请将文件分放到子目录",
		"ConfirmHeading":    "确认访问", "TargetDirectory": "目标目录别名", "CurrentDirectory": "当前目录别名",
		"ConfirmInstruction": "点击确认后验证并使用一次性访问码。仅打开页面不会登录。",
		"ReplaceSession":     "确认将进入目标目录，并替换当前浏览器已有的登录。",
		"ConfirmEnter":       "确认进入", "ManualEntry": "手动输入访问码",
		"InvalidLink":    "链接格式无效，请重新获取管理员提供的完整链接。",
		"PendingExpired": "确认页面已失效或验证失败。请重新打开原链接，或手动输入访问码；需要启用 Cookie。",
	},
	"en": {
		"Title": "File access", "Heading": "Access files",
		"Instruction": "Enter the one-time access code provided by your administrator.", "Code": "Access code",
		"Example": "e.g. ab12345678", "Enter": "Open directory", "Directory": "File directory",
		"Logout": "Log out", "Parent": "Parent directory", "Bytes": "bytes", "Download": "Download",
		"Empty": "This directory is empty", "Language": "Language", "Automatic": "Browser language",
		"InvalidSource": "Invalid client address", "Unavailable": "Service temporarily unavailable",
		"Blocked": "Access denied", "InvalidOrigin": "Invalid request origin",
		"TooManyRequests": "Too many requests; please try again later",
		"RateBanned":      "Too many attempts; your IP address is temporarily blocked",
		"FailureBanned":   "Too many incorrect codes; your IP address is temporarily blocked",
		"InvalidCode":     "The access code is invalid, already used or expired", "SessionLimit": "Session limit reached",
		"SaveFailed": "Unable to save login state; please try again later", "InvalidLogout": "Invalid logout request",
		"NotFound": "File or page not found", "ReadFailed": "Unable to read file",
		"Busy": "Service busy; please try again later", "InvalidRange": "Only one file range with a header of at most 128 bytes is supported",
		"DirectoryTooLarge": "Too many directory entries; organize files into subdirectories",
		"ConfirmHeading":    "Confirm access", "TargetDirectory": "Target directory alias", "CurrentDirectory": "Current directory alias",
		"ConfirmInstruction": "Confirm to validate and use the one-time access code. Opening this page does not log you in.",
		"ReplaceSession":     "Confirmation opens the target directory and replaces any existing login in this browser.",
		"ConfirmEnter":       "Confirm and enter", "ManualEntry": "Enter an access code manually",
		"InvalidLink":    "Invalid link format; request the complete link from your administrator.",
		"PendingExpired": "Confirmation expired or could not be verified. Reopen the original link or enter the code manually; cookies must be enabled.",
	},
}

func languagePage(r *http.Request) bool {
	return r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/access" || strings.HasPrefix(r.URL.Path, "/browse/"))
}

func languageChoice(r *http.Request) string {
	if languagePage(r) {
		switch choice := r.URL.Query().Get("lang"); choice {
		case "zh-CN", "en", "auto":
			return choice
		}
	}
	return ""
}

func saveLanguage(w http.ResponseWriter, r *http.Request) {
	choice := languageChoice(r)
	if choice == "" {
		return
	}
	cookie := &http.Cookie{Name: languageCookieName, Value: choice, Path: "/", MaxAge: 180 * 24 * 60 * 60,
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode}
	if choice == "auto" {
		cookie.Value = ""
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
}

func requestLanguage(r *http.Request) string {
	choice := languageChoice(r)
	if choice == "" {
		if cookie, err := r.Cookie(languageCookieName); err == nil {
			choice = cookie.Value
		}
	}
	if choice == "zh-CN" || choice == "en" {
		return choice
	}
	return browserLanguage(r.Header.Get("Accept-Language"))
}

func browserLanguage(header string) string {
	selected, best := "zh-CN", 0.0
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(item, ";")
		tag := strings.ToLower(strings.TrimSpace(parts[0]))
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(key, "q") {
				quality = 0
				break
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || !(q >= 0 && q <= 1) {
				quality = 0
				break
			}
			quality = q
		}
		language := ""
		switch {
		case tag == "zh" || strings.HasPrefix(tag, "zh-") || tag == "*":
			language = "zh-CN"
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			language = "en"
		}
		if language != "" && quality > best {
			selected, best = language, quality
		}
	}
	return selected
}

func languageHeaders(w http.ResponseWriter, language string) {
	w.Header().Set("Content-Language", language)
	w.Header().Add("Vary", "Accept-Language, Cookie")
}

func webError(w http.ResponseWriter, r *http.Request, key string, status int) {
	language := requestLanguage(r)
	languageHeaders(w, language)
	http.Error(w, translations[language][key], status)
}
