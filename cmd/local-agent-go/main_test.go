package main
import("context";"os";"path/filepath";"strings";"testing")
func TestFileBoundary(t *testing.T){
 dir:=t.TempDir()
 os.WriteFile(filepath.Join(dir,"ok.md"),[]byte("hello"),0600)
 os.WriteFile(filepath.Join(dir,".secret.md"),[]byte("secret"),0600)
 os.Symlink("/etc/passwd",filepath.Join(dir,"escape.md"))
 root,err:=os.OpenRoot(dir);if err!=nil{t.Fatal(err)};defer root.Close()
 a:=&Activities{Root:root}
 for _,p:=range []string{"../outside.md","/etc/passwd",".secret.md","escape.md"}{
  got,err:=a.Read(context.Background(),Decision{Action:"read_file",Path:p})
  if err!=nil||!strings.HasPrefix(got,"Tool error:"){t.Fatalf("%s: %q %v",p,got,err)}
 }
 got,err:=a.Read(context.Background(),Decision{Action:"read_file",Path:"ok.md"})
 if err!=nil||got!="hello"{t.Fatalf("%q %v",got,err)}
}
