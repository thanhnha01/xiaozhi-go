package main

import (
    "net/http"
    "os"
    "path/filepath"
    "strings"
)

// User-provided GIFs are an optional portable theme pack. No asset requires
// recompilation: users can replace the GIFs without changing device identity.
var mascotFiles = map[string]bool{
    "bom-dia.gif": true,
    "cute-dragon-love-you.gif": true,
    "qoobee.gif": true,
    "qoobee-cry.gif": true,
    "qoo-bee-qoo-bee-agapi.gif": true,
    "qoobee-upset.gif": true,
}

var allowedEmotions = map[string]bool{
    "neutral": true, "happy": true, "laughing": true, "funny": true,
    "loving": true, "sad": true, "crying": true, "angry": true,
    "surprised": true, "shocked": true, "thinking": true,
    "confused": true, "embarrassed": true, "confident": true,
    "sleepy": true, "relaxed": true, "winking": true, "silly": true,
}

func normalizeEmotion(value string) string {
    emotion := strings.ToLower(strings.TrimSpace(value))
    if !allowedEmotions[emotion] {
        return "neutral"
    }
    return emotion
}

func mascotAssetDir() string {
    if dir := strings.TrimSpace(os.Getenv("XIAOZHI_MASCOT_DIR")); dir != "" {
        return dir
    }
    exe, err := os.Executable()
    if err == nil {
        return filepath.Join(filepath.Dir(exe), "mascot")
    }
    return "mascot"
}

func (d *Dashboard) setEmotion(value string) {
    d.mu.Lock()
    d.emotion = normalizeEmotion(value)
    d.mu.Unlock()
}

func (d *Dashboard) serveMascot(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet && r.Method != http.MethodHead {
        http.Error(w, "Sai phương thức", http.StatusMethodNotAllowed)
        return
    }
    name := strings.TrimPrefix(r.URL.Path, "/assets/emotions/")
    if !mascotFiles[name] || strings.Contains(name, "/") {
        http.NotFound(w, r)
        return
    }
    f, err := os.Open(filepath.Join(d.mascotDir, name))
    if err != nil {
        http.NotFound(w, r)
        return
    }
    defer f.Close()
    info, err := f.Stat()
    if err != nil || !info.Mode().IsRegular() {
        http.NotFound(w, r)
        return
    }
    w.Header().Set("Content-Type", "image/gif")
    http.ServeContent(w, r, name, info.ModTime(), f)
}
