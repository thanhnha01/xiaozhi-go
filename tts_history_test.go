package main

import (
 "testing"
)

func TestJoinTTSChunksDecimals(t *testing.T) {
 cases:=[]struct{a,b,want string}{
  {"97,","58 triệu đồng mỗi lượng","97,58 triệu đồng mỗi lượng"},
  {"97.","58 triệu đồng mỗi lượng","97.58 triệu đồng mỗi lượng"},
  {"Giá mua 97,58 triệu.","Bán ra 107,48 triệu.","Giá mua 97,58 triệu. Bán ra 107,48 triệu."},
  {"Chào bạn,","giá hôm nay đây.","Chào bạn, giá hôm nay đây."},
  {"Một trăm triệu","không trăm năm mươi nghìn đồng","Một trăm triệu không trăm năm mươi nghìn đồng"},
  {"Hôm nay","!","Hôm nay!"},
 }
 for _,c:=range cases{if got:=joinTTSChunks(c.a,c.b);got!=c.want{t.Errorf("%q+%q: got %q want %q",c.a,c.b,got,c.want)}}
}

func TestTTSHistorySingleTurn(t *testing.T) {
 d:=&Dashboard{ttsEventIndex:-1}
 d.event("stt","Giá vàng PNJ?")
 d.beginTTSReply()
 d.appendTTSSentence("Giá vàng 18K mua vào 97,")
 d.appendTTSSentence("58 triệu đồng mỗi lượng,")
 d.appendTTSSentence("bán 107,48 triệu.")
 if len(d.events)!=2{t.Fatalf("expected STT + one TTS event, got %d",len(d.events))}
 if want:="Giá vàng 18K mua vào 97,58 triệu đồng mỗi lượng, bán 107,48 triệu.";d.events[1].Text!=want{t.Errorf("text %q want %q",d.events[1].Text,want)}
 d.endTTSReply()
 d.event("stt","Còn ngày mai?")
 d.beginTTSReply()
 d.appendTTSSentence("Ngày mai chưa có giá.")
 d.endTTSReply()
 if len(d.events)!=4{t.Fatalf("turns should not merge: %d",len(d.events))}
 if d.events[1].Text==d.events[3].Text{t.Fatal("merged replies incorrectly")}
}
func TestTTSHistoryCap(t *testing.T){
 d:=&Dashboard{ttsEventIndex:-1}
 for i:=0;i<125;i++{d.event("stt","thử")}
 d.beginTTSReply(); d.appendTTSSentence("97,");d.appendTTSSentence("58 triệu");d.endTTSReply()
 if len(d.events)!=120{t.Fatalf("expected cap 120 got %d",len(d.events))}
 if got:=d.events[len(d.events)-1].Text;got!="97,58 triệu"{t.Fatal(got)}
}
