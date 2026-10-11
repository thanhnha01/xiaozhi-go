package main

import "strings"

// formatServerAlert displays the actual message carried by XiaoZhi's
// type=alert frames. The upstream uses JSON "message", not "text".
func formatServerAlert(m *Message) string {
 if m==nil {return "Không nhận được chi tiết cảnh báo"}
 var parts []string
 for _,value:=range []string{m.AlertMessage,m.Text,m.Detail} {
  value=strings.TrimSpace(value)
  if value=="" {continue}
  duplicate:=false
  for _,seen:=range parts {if seen==value {duplicate=true;break}}
  if !duplicate {parts=append(parts,value)}
 }
 description:=strings.Join(parts," — ")
 if description=="" {description="Máy chủ không cung cấp nội dung"}
 if status:=strings.TrimSpace(m.Status);status!="" {description="["+status+"] "+description}
 if code:=strings.TrimSpace(m.Code);code!="" {description+=" (mã "+code+")"}
 return description
}
