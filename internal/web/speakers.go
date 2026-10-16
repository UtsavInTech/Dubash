// Copyright 2026 the Dubash contributors. Apache-2.0.
package web

import (
 "bytes"
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/binary"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "slices"
 "strings"
 "time"

 "github.com/UtsavInTech/dubash/internal/bank"
 "github.com/UtsavInTech/dubash/internal/bench"
 "github.com/UtsavInTech/dubash/internal/speaker"
)

const maxSpeakerUpload = 60 << 20
const termsVersion = "terms-v0.1"
const privacyVersion = "privacy-v0.1"
var speakerSlug = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Server) speakerRoutes(mux *http.ServeMux) {
 mux.HandleFunc("GET /speakers", s.page("speakers.html")); mux.HandleFunc("GET /terms", s.page("terms.html")); mux.HandleFunc("GET /privacy", s.page("privacy.html"))
 mux.HandleFunc("GET /api/admin/speakers", s.auth(s.withSpeakers(s.listSpeakers)))
 mux.HandleFunc("POST /api/admin/speakers", s.auth(s.withSpeakers(s.saveSpeaker)))
 mux.HandleFunc("GET /api/admin/speakers/{id}", s.auth(s.withSpeakers(s.getSpeaker)))
 mux.HandleFunc("POST /api/admin/speakers/{id}/consents", s.auth(s.withSpeakers(s.recordSpeakerConsent)))
 mux.HandleFunc("POST /api/admin/speakers/{id}/consents/{permission}/withdraw", s.auth(s.withSpeakers(s.withdrawSpeakerConsent)))
 mux.HandleFunc("GET /api/admin/speakers/{id}/samples", s.auth(s.withSpeakers(s.listSpeakerSamples)))
 mux.HandleFunc("POST /api/admin/speakers/{id}/samples", s.auth(s.withSpeakers(s.uploadSpeakerSample)))
 mux.HandleFunc("DELETE /api/admin/speakers/{id}/samples/{sample}", s.auth(s.withSpeakers(s.deleteSpeakerSample)))
 mux.HandleFunc("POST /api/admin/speakers/{id}/words", s.auth(s.withSpeakers(s.saveSpeakerWord)))
 mux.HandleFunc("PUT /api/admin/speakers/{id}/words/{entry}", s.auth(s.withSpeakers(s.saveSpeakerWord)))
}
func (s *Server) withSpeakers(next http.HandlerFunc) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){if s.Speakers==nil{http.Error(w,"speaker storage is not configured",503);return};next(w,r)} }
func (s *Server) listSpeakers(w http.ResponseWriter,r *http.Request){p,e:=s.Speakers.Profiles(r.Context());if e!=nil{http.Error(w,e.Error(),500);return};if p==nil{p=[]speaker.Profile{}};writeJSON(w,p)}
func (s *Server) saveSpeaker(w http.ResponseWriter,r *http.Request){var p speaker.Profile;if e:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20)).Decode(&p);e!=nil{http.Error(w,"invalid profile",400);return};p.DisplayName=strings.TrimSpace(p.DisplayName);if p.DisplayName==""||len(p.DisplayName)>160{http.Error(w,"display name is required (160 characters maximum)",400);return};if p.ID==""{base:=strings.Trim(speakerSlug.ReplaceAllString(strings.ToLower(p.DisplayName),"-"),"-");if base==""{base="speaker"};if len(base)>71{base=base[:71]};var suffix [4]byte;if _,err:=rand.Read(suffix[:]);err!=nil{http.Error(w,"could not create profile id",500);return};p.ID=base+"-"+hex.EncodeToString(suffix[:])};if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`).MatchString(p.ID){http.Error(w,"profile id must use lowercase letters, numbers and dashes",400);return};if slices.ContainsFunc(p.Languages,func(x string)bool{return !validSpeakerLanguage(x)}){http.Error(w,"unsupported speaker language",400);return};p.Languages=slices.Compact(p.Languages);if len(p.Languages)==0{http.Error(w,"choose at least one language",400);return};if !slices.Contains([]string{"discourse","comedy","politics","lecture","other"},p.Style){http.Error(w,"choose a supported speaking style",400);return};p.Notes=strings.TrimSpace(p.Notes);if len(p.Notes)>2000{http.Error(w,"notes are too long",400);return};out,e:=s.Speakers.SaveProfile(r.Context(),p);if e!=nil{http.Error(w,e.Error(),500);return};writeJSON(w,out)}
func validSpeakerLanguage(s string)bool{return slices.Contains([]string{"hi","en","ta","te","kn","ml","mr","bn","gu","pa"},s)}
func (s *Server) getSpeaker(w http.ResponseWriter,r *http.Request){p,e:=s.Speakers.Profile(r.Context(),r.PathValue("id"));if errors.Is(e,speaker.ErrNotFound){http.NotFound(w,r);return};if e!=nil{http.Error(w,e.Error(),500);return};cs,e:=s.Speakers.Consents(r.Context(),p.ID);if e!=nil{http.Error(w,e.Error(),500);return};xs,e:=s.Speakers.Samples(r.Context(),p.ID);if e!=nil{http.Error(w,e.Error(),500);return};public:=make([]speakerSampleView,0,len(xs));for _,x:=range xs{public=append(public,viewSample(x))};writeJSON(w,map[string]any{"profile":p,"consents":cs,"samples":public})}
func (s *Server) recordSpeakerConsent(w http.ResponseWriter,r *http.Request){id:=r.PathValue("id");if _,e:=s.Speakers.Profile(r.Context(),id);e!=nil{http.NotFound(w,r);return};var c speaker.Consent;if e:=json.NewDecoder(http.MaxBytesReader(w,r.Body,8192)).Decode(&c);e!=nil{http.Error(w,"invalid consent record",400);return};if !slices.Contains([]string{speaker.Recognition,speaker.Storage,speaker.SyntheticVoice,speaker.Terms},c.Permission){http.Error(w,"unknown consent permission",400);return};if c.By!="speaker"&&c.By!="organiser"{http.Error(w,"consent must be recorded by speaker or organiser",400);return};if c.By=="organiser"&&!c.OrganiserHasWrittenPermission{http.Error(w,"organiser must confirm written permission",400);return};if c.Permission==speaker.SyntheticVoice{http.Error(w,"synthetic voice is not available in Phase A and requires the speaker's own confirmation",409);return};if c.Permission==speaker.Terms{c.TextVersion=termsVersion+"/"+privacyVersion}else{c.TextVersion="speaker-consent-v0.1"};if c.Permission==speaker.Storage&&c.Granted&&!validStorageDays(c.RetentionDays){http.Error(w,"storage period must be 30, 90 or 365 days",400);return};s.speakerUploadMu.Lock();defer s.speakerUploadMu.Unlock();c.ProfileID=id;c.ID="";c.At=time.Now().UTC();prior,err:=s.Speakers.Consents(r.Context(),id);if err!=nil{http.Error(w,err.Error(),500);return};if len(prior)>0{last:=prior[len(prior)-1].At;if !c.At.After(last){c.At=last.Add(time.Microsecond)}};out,e:=s.Speakers.AddConsent(r.Context(),c);if e!=nil{http.Error(w,e.Error(),500);return};if !c.Granted{if e=s.deleteSpeakerSamples(r.Context(),id);e!=nil{http.Error(w,e.Error(),500);return};if c.Permission==speaker.Recognition{if e=s.retireSpeakerWords(r.Context(),id);e!=nil{http.Error(w,e.Error(),500);return}}};writeJSON(w,out)}
func latestGranted(cs []speaker.Consent,p string)bool{var latest *speaker.Consent;for i:=range cs{if cs[i].Permission==p&&(latest==nil||cs[i].At.After(latest.At)){latest=&cs[i]}};return latest!=nil&&latest.Granted}
func (s *Server) listSpeakerSamples(w http.ResponseWriter,r *http.Request){xs,e:=s.Speakers.Samples(r.Context(),r.PathValue("id"));if e!=nil{http.Error(w,e.Error(),500);return};if xs==nil{xs=[]speaker.Sample{}};public:=make([]speakerSampleView,0,len(xs));for _,x:=range xs{public=append(public,viewSample(x))};writeJSON(w,public)}
func (s *Server) uploadSpeakerSample(w http.ResponseWriter,r *http.Request){id:=r.PathValue("id");p,e:=s.Speakers.Profile(r.Context(),id);if e!=nil{http.NotFound(w,r);return};s.speakerUploadMu.Lock();defer s.speakerUploadMu.Unlock();cs,e:=s.Speakers.Consents(r.Context(),id);if e!=nil{http.Error(w,e.Error(),500);return};if !latestGranted(cs,speaker.Recognition)||!latestGranted(cs,speaker.Terms){http.Error(w,"recognition and terms consent are required before upload",403);return};if !latestGranted(cs,speaker.Storage){http.Error(w,"choose and grant a sample storage period before upload",403);return};r.Body=http.MaxBytesReader(w,r.Body,maxSpeakerUpload+(1<<20));if e=r.ParseMultipartForm(maxSpeakerUpload);e!=nil{http.Error(w,"upload exceeds 60 MB or is not multipart",413);return};defer r.MultipartForm.RemoveAll();f,h,e:=r.FormFile("file");if e!=nil{http.Error(w,"sample file is required",400);return};defer f.Close();if !validSampleSize(h.Size){http.Error(w,"sample file exceeds 60 MB",413);return};ext:=strings.ToLower(filepath.Ext(h.Filename));if !slices.Contains([]string{".wav",".mp3",".m4a"},ext){http.Error(w,"upload WAV, MP3 or M4A",400);return};data,e:=io.ReadAll(io.LimitReader(f,maxSpeakerUpload+1));if e!=nil||!validSampleSize(int64(len(data))){http.Error(w,"sample exceeds 60 MB",413);return};duration:=0.0;checks:=speaker.Checks{Message:"not analysed"};if ext==".wav"{pcm,err:=bench.DecodeWAV(bytes.NewReader(data));if err!=nil{http.Error(w,"WAV must be valid 16-bit PCM",400);return};duration=bench.Seconds(pcm);checks=analysePCM(pcm,duration)}else{duration,e=encodedDuration(data,ext);if e!=nil{http.Error(w,"could not read audio duration",400);return}};xs,e:=s.Speakers.Samples(r.Context(),id);if e!=nil{http.Error(w,e.Error(),500);return};if e:=validateSampleLimits(duration,xs);e!=nil{http.Error(w,e.Error(),400);return};days:=90;for _,c:=range cs{if c.Permission==speaker.Storage&&c.Granted&&c.RetentionDays>0{days=c.RetentionDays}};now:=time.Now().UTC();name:=filepath.Base(h.Filename);path:=filepath.Join(s.SpeakerDir,id,fmt.Sprintf("%d%s",now.UnixNano(),ext));if e=os.MkdirAll(filepath.Dir(path),0700);e!=nil{http.Error(w,"could not store sample",500);return};if e=os.WriteFile(path,data,0600);e!=nil{http.Error(w,"could not store sample",500);return};x:=speaker.Sample{ProfileID:p.ID,FileName:name,FilePath:path,Transcript:strings.TrimSpace(r.FormValue("transcript")),DurationSeconds:duration,Checks:checks,UploadedAt:now,DeleteAfter:now.AddDate(0,0,days)};x,e=s.Speakers.AddSample(r.Context(),x);if e!=nil{os.Remove(path);http.Error(w,e.Error(),500);return};writeJSON(w,viewSample(x))}

func validSampleSize(size int64) bool { return size >= 0 && size <= maxSpeakerUpload }
func validStorageDays(days int) bool { return slices.Contains([]int{30,90,365},days) }

func validateSampleLimits(duration float64, existing []speaker.Sample) error { if duration<60||duration>900{return errors.New("each sample must be between 1 and 15 minutes")};total:=duration;for _,x:=range existing{total+=x.DurationSeconds};if total>1800{return errors.New("speaker sample limit is 30 minutes total")};return nil }

func analysePCM(pcm []byte,duration float64)speaker.Checks{frames:=make([]float64,0,len(pcm)/640);peak,sum:=0.0,0.0;count:=0;frame:=int(bench.SampleRate/50)*2;for off:=0;off+1<len(pcm);off+=2{v:=float64(int16(binary.LittleEndian.Uint16(pcm[off:])))/32768;a:=math.Abs(v);if a>peak{peak=a};sum+=v*v;count++};for off:=0;off<len(pcm);off+=frame{end:=min(off+frame,len(pcm));energy:=0.0;n:=0;for i:=off;i+1<end;i+=2{v:=float64(int16(binary.LittleEndian.Uint16(pcm[i:])))/32768;energy+=v*v;n++};if n>0{frames=append(frames,energy/float64(n))}};slices.Sort(frames);noise,median:=0.0,0.0;if len(frames)>0{noise=frames[(len(frames)-1)/10];median=frames[len(frames)/2]};db:=func(v float64)float64{if v<=0{return -120};return 20*math.Log10(v)};peakDB,rmsDB,noiseDB:=db(peak),db(math.Sqrt(sum/float64(max(count,1)))),db(math.Sqrt(noise));ratio:=math.Sqrt(noise)/math.Sqrt(maxFloat(median,1e-12));msg:="Low background noise estimate";if ratio>.8{msg="Very noisy background estimate"}else if ratio>.5{msg="Some background noise"};return speaker.Checks{Analysed:true,DurationSeconds:duration,PeakDBFS:&peakDB,RMSDBFS:&rmsDB,NoiseDBFS:&noiseDB,Clipping:peakDB>-.5,Message:msg}}
func maxFloat(a,b float64)float64{if a>b{return a};return b}
func encodedDuration(b []byte,ext string)(float64,error){
 if ext==".mp3" { i:=0;if len(b)>10&&string(b[:3])=="ID3"{n:=int(b[6]&127)<<21|int(b[7]&127)<<14|int(b[8]&127)<<7|int(b[9]&127);i=10+n};seconds:=0.0
  for i+4<=len(b){if b[i]!=255||b[i+1]&224!=224{i++;continue};version:=int((b[i+1]>>3)&3);layer:=int((b[i+1]>>1)&3);bi:=int((b[i+2]>>4)&15);si:=int((b[i+2]>>2)&3);if version==1||layer!=1||bi==0||bi==15||si==3{i++;continue};rates:=[]int{44100,48000,32000};rate:=rates[si];if version==2{rate/=2}else if version==0{rate/=4};table:=[]int{0,32,40,48,56,64,80,96,112,128,160,192,224,256,320,0};samples:=1152;factor:=144;if version!=3{table=[]int{0,8,16,24,32,40,48,56,64,80,96,112,128,144,160,0};samples=576;factor=72};size:=factor*table[bi]*1000/rate+int((b[i+2]>>1)&1);if size<4{i++;continue};seconds+=float64(samples)/float64(rate);i+=size};if seconds>0{return seconds,nil};return 0,errors.New("MP3 has no audio frames")
 }
 for i:=0;i+32<len(b);i++{if string(b[i:i+4])!="mvhd"{continue};v:=b[i+4];if v==0{scale:=binary.BigEndian.Uint32(b[i+16:]);d:=binary.BigEndian.Uint32(b[i+20:]);if scale>0{return float64(d)/float64(scale),nil}}else if v==1{scale:=binary.BigEndian.Uint32(b[i+20:]);d:=binary.BigEndian.Uint64(b[i+24:]);if scale>0{return float64(d)/float64(scale),nil}}};return 0,errors.New("M4A duration not found")
}
func (s *Server) deleteSpeakerSamples(ctx context.Context,id string)error{xs,e:=s.Speakers.Samples(ctx,id);if e!=nil{return e};for _,x:=range xs{if e=os.Remove(x.FilePath);e!=nil&&!os.IsNotExist(e){return e};if _,e=s.Speakers.DeleteSample(ctx,id,x.ID);e!=nil{return e}};return nil}
func (s *Server) deleteSpeakerSample(w http.ResponseWriter,r *http.Request){id,sid:=r.PathValue("id"),r.PathValue("sample");xs,e:=s.Speakers.Samples(r.Context(),id);if e!=nil{http.Error(w,e.Error(),500);return};var found *speaker.Sample;for i:=range xs{if xs[i].ID==sid{found=&xs[i];break}};if found==nil{http.NotFound(w,r);return};if e=os.Remove(found.FilePath);e!=nil&&!os.IsNotExist(e){http.Error(w,"could not remove sample file",500);return};if _,e=s.Speakers.DeleteSample(r.Context(),id,sid);e!=nil{http.Error(w,e.Error(),500);return};w.WriteHeader(http.StatusNoContent)}
func (s *Server) retireSpeakerWords(ctx context.Context,id string)error{if s.Bank==nil{return nil};es,e:=s.Bank.Entries(ctx,bank.Filter{Scope:bank.ScopeSpeaker,ScopeID:id});if e!=nil{return e};for _,x:=range es{x.Status=bank.Retired;if _,e=s.Bank.SaveEntry(ctx,x,"consent-withdrawal");e!=nil{return e}};return nil}
func (s *Server) saveSpeakerWord(w http.ResponseWriter,r *http.Request){id:=r.PathValue("id");profile,e:=s.Speakers.Profile(r.Context(),id);if e!=nil{http.NotFound(w,r);return};s.speakerUploadMu.Lock();defer s.speakerUploadMu.Unlock();var q struct{Term string `json:"term"`;Lang string `json:"lang"`;Meaning string `json:"meaning"`;Translations map[string]string `json:"translations"`;Misheard []string `json:"misheard"`};if e:=json.NewDecoder(http.MaxBytesReader(w,r.Body,64<<10)).Decode(&q);e!=nil{http.Error(w,"invalid phrase",400);return};cs,e:=s.Speakers.Consents(r.Context(),id);if e!=nil{http.Error(w,e.Error(),500);return};if !latestGranted(cs,speaker.Recognition){http.Error(w,"recognition consent is required before adding speaker phrases",403);return};if s.Bank==nil{http.Error(w,"Dictation Bank unavailable",503);return};if q.Lang==""&&len(profile.Languages)>0{q.Lang=profile.Languages[0]};entryID:=r.PathValue("entry");if entryID!=""{existing,err:=s.Bank.Entry(r.Context(),entryID);if err!=nil||existing.Scope!=bank.ScopeSpeaker||existing.ScopeID!=id{http.NotFound(w,r);return}};entry:=bank.Entry{ID:entryID,Scope:bank.ScopeSpeaker,ScopeID:id,Term:q.Term,Lang:q.Lang,Meaning:q.Meaning,Translations:q.Translations,Misheard:q.Misheard,Status:bank.Suggested,Source:bank.SourceManual,CreatedBy:actor};out,e:=s.Bank.SaveEntry(r.Context(),entry,actor);if e!=nil{bankError(w,e);return};writeJSON(w,out)}

type speakerSampleView struct { ID string `json:"id"`; ProfileID string `json:"profileId"`; FileName string `json:"fileName"`; Transcript string `json:"transcript,omitempty"`; DurationSeconds float64 `json:"durationSeconds"`; Checks speaker.Checks `json:"checks"`; UploadedAt time.Time `json:"uploadedAt"`; DeleteAfter time.Time `json:"deleteAfter"` }
func viewSample(x speaker.Sample) speakerSampleView { return speakerSampleView{ID:x.ID,ProfileID:x.ProfileID,FileName:x.FileName,Transcript:x.Transcript,DurationSeconds:x.DurationSeconds,Checks:x.Checks,UploadedAt:x.UploadedAt,DeleteAfter:x.DeleteAfter} }

func (s *Server) withdrawSpeakerConsent(w http.ResponseWriter,r *http.Request) {
 var req struct { By string `json:"by"`; Written bool `json:"organiserHasWrittenPermission"` }
 if err:=json.NewDecoder(http.MaxBytesReader(w,r.Body,2048)).Decode(&req);err!=nil { http.Error(w,"invalid withdrawal",400);return }
 body,_:=json.Marshal(speaker.Consent{Permission:r.PathValue("permission"),Granted:false,By:req.By,OrganiserHasWrittenPermission:req.Written})
 clone:=r.Clone(r.Context());clone.Body=io.NopCloser(bytes.NewReader(body));s.recordSpeakerConsent(w,clone)
}
