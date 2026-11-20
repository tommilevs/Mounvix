package app

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/tommilevs/Mounvix/server/internal/feeds"
)

type Root struct {
	Name string `json:"name"`
	Path string `json:"path"`
	ReadOnly bool `json:"read_only"`
}
type FeedConfig = feeds.Config

type Config struct {
	Port int `json:"port"`
	DataDir string `json:"data_dir"`
	Lang string `json:"lang"`
	AIBaseURL string `json:"ai_base_url,omitempty"`
	AIModel string `json:"ai_model,omitempty"`
	AIKey string `json:"ai_key,omitempty"`
	Roots []Root `json:"roots"`
	Feeds FeedConfig `json:"feeds"`
	PluginDir string `json:"plugin_dir,omitempty"`
	PublicBasePath string `json:"public_base_path,omitempty"`
	PublicScheme string `json:"public_scheme,omitempty"`
}
func DefaultConfig()Config{return Config{Port:48100,DataDir:"data",Lang:"ru",Roots:[]Root{{Name:"home",Path:"/",ReadOnly:false}},Feeds:feeds.DefaultConfig()}}
func LoadConfig(path string)(Config,string,error){
	cfg:=DefaultConfig();p:=path;if p==""{p=filepath.Join(cfg.DataDir,"config.json")}
	b,err:=os.ReadFile(p);if err==nil{if err=json.Unmarshal(b,&cfg);err!=nil{return cfg,p,err};if cfg.Port==0{cfg.Port=48100};if cfg.Lang==""{cfg.Lang="ru"};return cfg,p,nil}
	if !os.IsNotExist(err){return cfg,p,err};if err=os.MkdirAll(cfg.DataDir,0o755);err!=nil{return cfg,p,err};if err=SaveConfig(p,cfg);err!=nil{return cfg,p,err};return cfg,p,nil
}
func SaveConfig(path string,cfg Config)error{b,err:=json.MarshalIndent(cfg,"","  ");if err!=nil{return err};if err=os.MkdirAll(filepath.Dir(path),0o755);err!=nil{return err};tmp:=path+".tmp";if err=os.WriteFile(tmp,b,0o600);err!=nil{return err};return os.Rename(tmp,path)}

func pluginDirForConfig(cfg Config,cfgPath,dataDir string)string{
	if cfg.PluginDir!=""{return cfg.PluginDir}
	switch filepath.Clean(cfgPath){case "/opt/etc/mounvix/config.json":return "/opt/etc/mounvix/plugins";case "/etc/mounvix/config.json":return "/etc/mounvix/plugins";default:return filepath.Join(dataDir,"plugins")}
}
func validatePublicBasePath(value string)error{
	if value==""{return nil}
	if !strings.HasPrefix(value,"/")||value=="/"||strings.HasPrefix(value,"//")||strings.HasSuffix(value,"/")||strings.ContainsAny(value,"\\?#%")||strings.Contains(value,"//"){return errors.New("public_base_path must be a canonical slash-prefixed mount path")}
	decoded,err:=url.PathUnescape(value);if err!=nil||decoded!=value{return errors.New("public_base_path must not contain encoded paths")}
	for _,part:=range strings.Split(strings.TrimPrefix(value,"/"),"/"){if part==""||part=="."||part==".."{return errors.New("public_base_path contains an unsafe path segment")};for _,r:=range part{if !(r>='a'&&r<='z'||r>='A'&&r<='Z'||r>='0'&&r<='9'||r=='-'||r=='_'||r=='.'){return errors.New("public_base_path contains an invalid character")}}}
	return nil
}
func validatePublicScheme(value string)error{if value!=""&&value!="http"&&value!="https"{return errors.New("public_scheme must be http or https")};return nil}
