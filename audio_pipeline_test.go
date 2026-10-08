package main
import (
 "math"
 "testing"
 "github.com/hraban/opus"
)

func TestVolumeClampedAndPCM(t *testing.T){
 a:=&AudioManager{volume:0.7}
 a.SetVolume(2)
 if a.Volume()!=1 {t.Fatalf("volume above 100%%: %v",a.Volume())}
 a.SetVolume(-0.3)
 if a.Volume()!=0 {t.Fatalf("volume below 0: %v",a.Volume())}
 samples:=[]int16{10000,-10000}
 applyVolume(samples,0.5)
 if samples[0]!=5000||samples[1]!=-5000 {t.Fatalf("attenuation error: %v",samples)}
}
func TestMicLevel(t *testing.T) {
 if pcmLevel(nil)!=0||pcmLevel(make([]int16,960))!=0 {t.Fatal("silence not zero")}
 if got:=pcmLevel([]int16{32767,-32768});got<95||got>100{t.Fatalf("unexpected peak level: %d",got)}
}
func TestMicrophonePCMToOpusDispatch(t *testing.T) {
 enc,err:=opus.NewEncoder(audioSampleRate,audioChannels,opus.AppVoIP)
 if err!=nil{t.Fatal(err)}
 a:=&AudioManager{enc:enc}
 listening:=false
 a.SetStateCallback(func() State {if listening{return StateListening};return StateIdle})
 var frames [][]byte
 a.SetSendCallback(func(frame []byte){frames=append(frames,append([]byte(nil),frame...))})
 pcm:=make([]int16,audioSampleRate*frameDurationMs/1000)
 for i:=range pcm {pcm[i]=int16(6000*math.Sin(float64(i)*2*math.Pi*440/audioSampleRate))}
 a.inputCallback(pcm,nil)
 if len(frames)!=0{t.Fatal("idle microphone unexpectedly dispatched audio")}
 listening=true
 a.inputCallback(pcm,nil)
 if len(frames)!=1||len(frames[0])==0 {t.Fatal("listening did not encode/dispatch Opus")}
 if a.MicLevel()==0 {t.Fatal("microphone signal level missing")}
 dec,err:=opus.NewDecoder(audioSampleRate,audioChannels)
 if err!=nil{t.Fatal(err)}
 decoded:=make([]int16,len(pcm))
 n,err:=dec.Decode(frames[0],decoded)
 if err!=nil||n==0{t.Fatalf("sent Opus frame not decodable: %d, %v",n,err)}
}