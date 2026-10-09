package main

import (
 "strings"
 "testing"
)

func TestConsumerDashboardHidesTechnicalPanels(t *testing.T) {
 raw,err:=dashboardAssets.ReadFile("web/dashboard.html")
 if err!=nil{t.Fatal(err)}
 html:=string(raw)
 for _,want:=range []string{"page-home","page-chat","page-music","page-settings","activationOverlay","Tiểu Trí","qoo-bee-qoo-bee-agapi.gif"}{
  if !strings.Contains(html,want){t.Errorf("missing consumer UI element %q",want)}
 }
 for _,removed:=range []string{"Nhật ký kỹ thuật","Địa chỉ MAC","Phiên làm việc","Máy chủ MCP","micFramesDropped","framesDropped"}{
  if strings.Contains(html,removed){t.Errorf("debug indicator leaked into consumer UI: %q",removed)}
 }
 if !strings.Contains(html,`idle:"qoobee.gif"`) || !strings.Contains(html,`speaking:"bom-dia.gif"`) {
  t.Fatal("idle/speaking mascot mapping not swapped")
 }
 if !strings.Contains(html,`$("activationOverlay").classList.toggle("hidden",!hasCode)`){
  t.Fatal("pairing overlay must depend on actual activation code")
 }
}
