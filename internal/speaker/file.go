package speaker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type fileData struct { Profiles []Profile `json:"profiles"`; Consents []Consent `json:"consents"`; Samples []Sample `json:"samples"` }
type FileStore struct { path string; mu sync.Mutex; data fileData }
func OpenFile(path string) (*FileStore,error) { s:=&FileStore{path:path}; b,e:=os.ReadFile(path); if e==nil { e=json.Unmarshal(b,&s.data) }; if e!=nil && !os.IsNotExist(e) { return nil,fmt.Errorf("speaker: %w",e) }; return s,nil }
func newID() string { var b [12]byte; _,_=rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func (s *FileStore) save() error { b,e:=json.MarshalIndent(s.data,""," "); if e!=nil{return e}; if e=os.MkdirAll(filepath.Dir(s.path),0700); e!=nil{return e}; tmp:=s.path+".tmp"; if e=os.WriteFile(tmp,b,0600); e!=nil{return e}; return os.Rename(tmp,s.path) }
func (s *FileStore) SaveProfile(_ context.Context,p Profile)(Profile,error){ s.mu.Lock(); defer s.mu.Unlock(); now:=time.Now().UTC(); if p.ID=="" {return p,fmt.Errorf("speaker: id required")}; for i,x:=range s.data.Profiles {if x.ID==p.ID {p.CreatedAt=x.CreatedAt; p.UpdatedAt=now; s.data.Profiles[i]=p; return p,s.save()}}; p.CreatedAt=now;p.UpdatedAt=now;s.data.Profiles=append(s.data.Profiles,p);return p,s.save() }
func (s *FileStore) Profiles(_ context.Context)([]Profile,error){s.mu.Lock();defer s.mu.Unlock();return slices.Clone(s.data.Profiles),nil}
func (s *FileStore) Profile(_ context.Context,id string)(Profile,error){s.mu.Lock();defer s.mu.Unlock();for _,p:=range s.data.Profiles{if p.ID==id{return p,nil}};return Profile{},ErrNotFound}
func (s *FileStore) AddConsent(_ context.Context,c Consent)(Consent,error){s.mu.Lock();defer s.mu.Unlock();if c.ID==""{c.ID=newID()};if c.At.IsZero(){c.At=time.Now().UTC()};s.data.Consents=append(s.data.Consents,c);return c,s.save()}
func (s *FileStore) Consents(_ context.Context,id string)([]Consent,error){s.mu.Lock();defer s.mu.Unlock();var out []Consent;for _,c:=range s.data.Consents{if c.ProfileID==id{out=append(out,c)}};return out,nil}
func (s *FileStore) AddSample(_ context.Context,x Sample)(Sample,error){s.mu.Lock();defer s.mu.Unlock();if x.ID==""{x.ID=newID()};s.data.Samples=append(s.data.Samples,x);return x,s.save()}
func (s *FileStore) Samples(_ context.Context,id string)([]Sample,error){s.mu.Lock();defer s.mu.Unlock();var out []Sample;for _,x:=range s.data.Samples{if x.ProfileID==id{out=append(out,x)}};return out,nil}
func (s *FileStore) DeleteSample(_ context.Context,id,sid string)(Sample,error){s.mu.Lock();defer s.mu.Unlock();for i,x:=range s.data.Samples{if x.ProfileID==id&&x.ID==sid{s.data.Samples=append(s.data.Samples[:i],s.data.Samples[i+1:]...);if e:=s.save();e!=nil{return Sample{},e};return x,nil}};return Sample{},ErrNotFound}
func (s *FileStore) DeleteExpired(_ context.Context,now time.Time)([]Sample,error){s.mu.Lock();defer s.mu.Unlock();var expired,keep []Sample;for _,x:=range s.data.Samples{if !x.DeleteAfter.IsZero()&&!x.DeleteAfter.After(now){expired=append(expired,x)}else{keep=append(keep,x)}};s.data.Samples=keep;if len(expired)>0{if e:=s.save();e!=nil{return nil,e}};return expired,nil}
func(*FileStore)Close()error{return nil}
