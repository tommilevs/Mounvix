package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const auditRetention = 1000
const auditRetentionAge = 30 * 24 * time.Hour

type AuditEvent struct {
	Seq int64 `json:"seq"`
	At time.Time `json:"at"`
	Actor string `json:"actor"`
	Phase string `json:"phase"`
	Action string `json:"action"`
	PluginID string `json:"plugin_id"`
	Source PluginSource `json:"source,omitempty"`
	RunID string `json:"run_id,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}
type auditState struct { LatestSeq int64 `json:"latest_seq"`; Items []AuditEvent `json:"items"` }
type AuditStore struct { mu sync.Mutex; path string; state auditState }

func NewAuditStore(dataDir string) (*AuditStore,error) {
	if err:=os.MkdirAll(dataDir,0o755); err!=nil{return nil,err}
	s:=&AuditStore{path:filepath.Join(dataDir,"plugin-audit.json")}
	b,err:=os.ReadFile(s.path)
	if err==nil { if err=json.Unmarshal(b,&s.state); err!=nil{return nil,err}; if s.state.Items==nil{s.state.Items=[]AuditEvent{}}; return s,nil }
	if !errors.Is(err,os.ErrNotExist){return nil,err}
	s.state.Items=[]AuditEvent{}
	return s,nil
}
func (s *AuditStore) Append(event AuditEvent) error {
	s.mu.Lock(); defer s.mu.Unlock()
	if event.At.IsZero(){event.At=time.Now().UTC()} else {event.At=event.At.UTC()}
	s.state.LatestSeq++
	event.Seq=s.state.LatestSeq
	s.state.Items=append(s.state.Items,event)
	s.trimLocked(time.Now().UTC())
	return s.saveLocked()
}
func (s *AuditStore) Recent(limit int)([]AuditEvent,error){
	s.mu.Lock();defer s.mu.Unlock()
	if limit<=0{return []AuditEvent{},nil}
	start:=0;if len(s.state.Items)>limit{start=len(s.state.Items)-limit}
	out:=append([]AuditEvent(nil),s.state.Items[start:]...)
	return out,nil
}
func(s *AuditStore) trimLocked(now time.Time){
	kept:=s.state.Items[:0]
	for _,e:=range s.state.Items{if now.Sub(e.At)<=auditRetentionAge{kept=append(kept,e)}}
	if len(kept)>auditRetention{kept=kept[len(kept)-auditRetention:]}
	s.state.Items=kept
	// Keep records in ascending sequence order even after loading a diagnostic journal.
	sort.Slice(s.state.Items,func(i,j int)bool{return s.state.Items[i].Seq<s.state.Items[j].Seq})
}
func(s *AuditStore) saveLocked()error{
	b,err:=json.MarshalIndent(s.state,"","  ");if err!=nil{return err}
	tmp:=s.path+".tmp"
	if err=os.WriteFile(tmp,b,0o600);err!=nil{return err}
	return os.Rename(tmp,s.path)
}
