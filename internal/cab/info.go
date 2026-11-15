package cab

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Info: fotografia del cabinato raccolta con un solo comando SSH.
type Info struct {
	CollectedAt                              time.Time           `json:"collected_at"`
	Hostname                                 string              `json:"hostname"`
	Version                                  string              `json:"version"`
	Arch                                     string              `json:"arch"`
	CPUModel                                 string              `json:"cpu_model"`
	CPUCount                                 int                 `json:"cpu_count"`
	CPUFlags                                 string              `json:"-"`
	Governor                                 string              `json:"governor"`
	MemTotal                                 int64               `json:"mem_total"`
	MemUsed                                  int64               `json:"mem_used"`
	DiskTotal                                int64               `json:"disk_total"`
	DiskUsed                                 int64               `json:"disk_used"`
	DiskFree                                 int64               `json:"disk_free"`
	CPUTempC                                 float64             `json:"cpu_temp_c"`
	ResolutionCur                            string              `json:"resolution"`
	RefreshHz                                float64             `json:"refresh_hz"`
	Xrandr                                   string              `json:"-"`
	UptimeSec                                float64             `json:"uptime_sec"`
	IPs                                      []string            `json:"ips"`
	MAC                                      string              `json:"mac"`
	Wifi                                     bool                `json:"wifi"`
	InputDevices                             []string            `json:"input_devices"`
	Joysticks                                []Joystick          `json:"joysticks"`
	ESInput                                  []InputConfig       `json:"es_input"`
	BatoceraConf                             map[string]string   `json:"batocera_conf"`
	ESSettings                               map[string]string   `json:"es_settings"`
	Cores                                    []string            `json:"cores"`
	MameVersion                              string              `json:"mame_version"`
	RomCounts                                map[string]int      `json:"rom_counts"`
	RomTotal                                 int                 `json:"rom_total"`
	BiosMissing                              []BiosItem          `json:"bios_missing"`
	BiosOK                                   int                 `json:"bios_ok"`
	Services                                 map[string]bool     `json:"services"` // nome → abilitato
	Systems                                  map[string]ESSystem `json:"systems"`
	Ffmpeg, Ffprobe, Xdotool, Rsync, Flatpak bool
	AudioCards                               string            `json:"-"`
	AudioOut                                 string            `json:"audio_out"`
	Hotkeys                                  string            `json:"-"`
	ESLog                                    string            `json:"-"`
	Dmesg                                    string            `json:"-"`
	Processes                                string            `json:"-"`
	Samba                                    bool              `json:"samba"`
	ESApiOpen                                bool              `json:"es_api_open"`
	Timezone                                 string            `json:"timezone"`
	Kernel                                   string            `json:"kernel"`
	GPU                                      string            `json:"gpu"`
	Companion                                map[string]string `json:"companion"`
}

type Joystick struct {
	Event string `json:"event"`
	Name  string `json:"name"`
	Phys  string `json:"phys"`
	ID    string `json:"id"`
	GUID  string `json:"guid"`
}

type InputConfig struct {
	Type       string            `json:"type"`
	DeviceName string            `json:"device_name"`
	GUID       string            `json:"guid"`
	Inputs     map[string]string `json:"inputs"` // nome → "button:3" / "hat:0:1" / "axis:1:-1" / "key:13"
}

type BiosItem struct {
	System string `json:"system"`
	Path   string `json:"path"`
	Status string `json:"status"` // MISSING | INVALID
	Detail string `json:"detail"`
}

type ESSystem struct {
	Name       string              `json:"name"`
	Fullname   string              `json:"fullname"`
	Path       string              `json:"path"`
	Extensions []string            `json:"extensions"`
	Emulators  map[string][]string `json:"emulators"` // emulatore → core
	Default    string              `json:"default"`   // "emulatore/core" di default
}

const infoScript = `
echo "@@hostname"; hostname
echo "@@version"; cat /usr/share/batocera/batocera.version 2>/dev/null | head -1
echo "@@arch"; cat /usr/share/batocera/batocera.arch 2>/dev/null || uname -m
echo "@@kernel"; uname -r
echo "@@cpu"; grep -m1 'model name' /proc/cpuinfo | cut -d: -f2; grep -c ^processor /proc/cpuinfo; grep -m1 '^flags' /proc/cpuinfo | cut -d: -f2
echo "@@governor"; cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null
echo "@@mem"; grep -E 'MemTotal|MemAvailable' /proc/meminfo
echo "@@disk"; df -B1 /userdata 2>/dev/null | tail -1
echo "@@temp"; for z in /sys/class/thermal/thermal_zone*; do t=$(cat $z/type 2>/dev/null); v=$(cat $z/temp 2>/dev/null); echo "$t $v"; done
echo "@@xrandr"; DISPLAY=:0 xrandr 2>/dev/null | head -40
echo "@@uptime"; cat /proc/uptime
echo "@@ip"; ip -4 -o addr 2>/dev/null | grep -v ' lo ' | awk '{print $2, $4}'
echo "@@mac"; for i in /sys/class/net/e*; do cat $i/address 2>/dev/null; break; done
echo "@@wifi"; ls /sys/class/net | grep -c '^wl'
echo "@@input"; for e in /sys/class/input/event*; do n=$(cat $e/device/name 2>/dev/null); p=$(udevadm info -q property -p $e 2>/dev/null); j=$(echo "$p" | grep -c 'ID_INPUT_JOYSTICK=1'); echo "$(basename $e)|$n|$j|$(cat $e/device/phys 2>/dev/null)|$(cat $e/device/id/vendor 2>/dev/null):$(cat $e/device/id/product 2>/dev/null)"; done
echo "@@esinput"; cat /userdata/system/configs/emulationstation/es_input.cfg 2>/dev/null
echo "@@conf"; grep -v '^\s*#' /userdata/system/batocera.conf 2>/dev/null | grep '='
echo "@@essettings"; cat /userdata/system/configs/emulationstation/es_settings.cfg 2>/dev/null
echo "@@cores"; ls /usr/lib/libretro/ 2>/dev/null | sed 's/_libretro.so//'
echo "@@mame"; /usr/bin/mame/mame -version 2>/dev/null | head -1
echo "@@roms"; for d in /userdata/roms/*/; do n=$(basename "$d"); c=$(find "$d" -maxdepth 2 -type f ! -name '*.txt' ! -name '*.xml' ! -name '*.cfg' ! -name '*.jpg' ! -name '*.png' ! -name '*.mp4' ! -name '*.pdf' ! -path '*/images/*' ! -path '*/videos/*' ! -path '*/manuals/*' ! -path '*/media/*' ! -path '*/downloaded_*' 2>/dev/null | wc -l); echo "$n $c"; done
echo "@@bios"; batocera-systems 2>/dev/null
echo "@@services"; ls /userdata/system/services 2>/dev/null; echo "--"; batocera-services list 2>/dev/null
echo "@@essystems"; cat /usr/share/emulationstation/es_systems.cfg 2>/dev/null; ls /userdata/system/configs/emulationstation/es_systems_*.cfg 2>/dev/null | while read f; do cat "$f"; done
echo "@@tools"; for t in ffmpeg ffprobe xdotool rsync flatpak; do which $t >/dev/null 2>&1 && echo "$t"; done
echo "@@audio"; cat /proc/asound/cards 2>/dev/null; echo "--"; batocera-settings-get audio.device 2>/dev/null
echo "@@hotkeys"; cat /userdata/system/configs/evmapy/hotkeys.keys 2>/dev/null | head -5; batocera-joysticks-hotkeys 2>/dev/null | head -20
echo "@@eslog"; tail -n 30 /userdata/system/logs/es_launch_stderr.log 2>/dev/null
echo "@@dmesg"; dmesg 2>/dev/null | grep -iE 'i915|drm|error|fail' | tail -15
echo "@@ps"; ps -eo pcpu,comm --sort=-pcpu 2>/dev/null | head -8
echo "@@samba"; pgrep -x smbd >/dev/null && echo yes
echo "@@esapi"; (ss -ltn 2>/dev/null || netstat -ltn 2>/dev/null) | grep -q ':1234 ' && echo listening; batocera-settings-get system.es.api 2>/dev/null
echo "@@tz"; batocera-settings-get system.timezone 2>/dev/null
echo "@@gpu"; lspci 2>/dev/null | grep -i vga | head -1
echo "@@companion"; for f in /userdata/system/companion/version /userdata/system/companion/volume-buttons.json /userdata/system/companion/wine-keys.json /userdata/system/companion-record.sh /userdata/system/companion/ha-agent.sh; do [ -e "$f" ] && echo "$(basename $f)=1"; done; batocera-services list 2>/dev/null | grep -i companion; ls /userdata/system/services 2>/dev/null | grep -iE 'companion|volume|ha_|homeassistant' | sed 's/^/svc:/'
`

func (s *SSH) CollectInfo() (*Info, error) {
	res, err := s.RunTimeout(infoScript, 3*time.Minute)
	if err != nil {
		return nil, err
	}
	sec := SplitSections(res.Stdout)
	in := &Info{CollectedAt: time.Now(), BatoceraConf: map[string]string{}, ESSettings: map[string]string{}, RomCounts: map[string]int{}, Services: map[string]bool{}, Systems: map[string]ESSystem{}, Companion: map[string]string{}}
	in.Hostname = strings.TrimSpace(sec["hostname"])
	in.Version = strings.TrimSpace(sec["version"])
	if f := strings.Fields(in.Version); len(f) > 0 {
		in.Version = f[0]
	}
	in.Arch = strings.TrimSpace(sec["arch"])
	in.Kernel = strings.TrimSpace(sec["kernel"])
	if l := Lines(sec["cpu"]); len(l) >= 2 {
		in.CPUModel = strings.TrimSpace(l[0])
		in.CPUCount, _ = strconv.Atoi(strings.TrimSpace(l[1]))
		if len(l) > 2 {
			in.CPUFlags = l[2]
		}
	}
	in.Governor = strings.TrimSpace(sec["governor"])
	for _, l := range Lines(sec["mem"]) {
		f := strings.Fields(l)
		if len(f) >= 2 {
			kb, _ := strconv.ParseInt(f[1], 10, 64)
			if strings.HasPrefix(l, "MemTotal") {
				in.MemTotal = kb * 1024
			} else {
				in.MemUsed = in.MemTotal - kb*1024
			}
		}
	}
	if f := strings.Fields(sec["disk"]); len(f) >= 4 {
		in.DiskTotal, _ = strconv.ParseInt(f[1], 10, 64)
		in.DiskUsed, _ = strconv.ParseInt(f[2], 10, 64)
		in.DiskFree, _ = strconv.ParseInt(f[3], 10, 64)
	}
	for _, l := range Lines(sec["temp"]) {
		f := strings.Fields(l)
		if len(f) == 2 {
			v, _ := strconv.ParseFloat(f[1], 64)
			if strings.Contains(f[0], "x86_pkg") || in.CPUTempC == 0 {
				in.CPUTempC = v / 1000
			}
		}
	}
	in.Xrandr = sec["xrandr"]
	if m := regexp.MustCompile(`(\d+x\d+)\s+(\d+\.\d+)\*`).FindStringSubmatch(in.Xrandr); m != nil {
		in.ResolutionCur = m[1]
		in.RefreshHz, _ = strconv.ParseFloat(m[2], 64)
	} else if m := regexp.MustCompile(`current (\d+) x (\d+)`).FindStringSubmatch(in.Xrandr); m != nil {
		in.ResolutionCur = m[1] + "x" + m[2]
	}
	if f := strings.Fields(sec["uptime"]); len(f) > 0 {
		in.UptimeSec, _ = strconv.ParseFloat(f[0], 64)
	}
	for _, l := range Lines(sec["ip"]) {
		f := strings.Fields(l)
		if len(f) == 2 {
			in.IPs = append(in.IPs, strings.Split(f[1], "/")[0]+" ("+f[0]+")")
		}
	}
	in.MAC = strings.TrimSpace(sec["mac"])
	in.Wifi = strings.TrimSpace(sec["wifi"]) != "0" && strings.TrimSpace(sec["wifi"]) != ""
	for _, l := range Lines(sec["input"]) {
		f := strings.Split(l, "|")
		if len(f) < 3 {
			continue
		}
		name := strings.TrimSpace(f[1])
		if name == "" && strings.Contains(f[0], "*") {
			continue
		}
		in.InputDevices = append(in.InputDevices, name+" ("+f[0]+")")
		if strings.TrimSpace(f[2]) == "1" {
			j := Joystick{Event: f[0], Name: name}
			if len(f) > 3 {
				j.Phys = f[3]
			}
			if len(f) > 4 {
				j.ID = f[4]
			}
			in.Joysticks = append(in.Joysticks, j)
		}
	}
	in.ESInput = ParseESInput(sec["esinput"])
	for _, l := range Lines(sec["conf"]) {
		if k, v, ok := strings.Cut(l, "="); ok {
			in.BatoceraConf[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, m := range reEsSetting.FindAllStringSubmatch(sec["essettings"], -1) {
		in.ESSettings[m[1]] = m[2]
	}
	in.Cores = Lines(sec["cores"])
	in.MameVersion = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sec["mame"]), "MAME v"))
	for _, l := range Lines(sec["roms"]) {
		f := strings.Fields(l)
		if len(f) == 2 {
			n, _ := strconv.Atoi(f[1])
			in.RomCounts[f[0]] = n
			in.RomTotal += n
		}
	}
	in.BiosMissing, in.BiosOK = ParseBios(sec["bios"])
	svcPart := strings.SplitN(sec["services"], "--", 2)
	for _, l := range Lines(svcPart[0]) {
		in.Services[strings.TrimSpace(l)] = false
	}
	if len(svcPart) > 1 {
		for _, l := range Lines(svcPart[1]) {
			f := strings.Fields(l)
			if len(f) >= 1 {
				in.Services[f[0]] = len(f) > 1 && strings.Contains(strings.ToLower(f[1]), "*") || strings.Contains(l, "enabled") || strings.HasPrefix(l, "*")
				if strings.HasPrefix(l, "*") && len(f) > 1 {
					in.Services[f[1]] = true
				}
			}
		}
	}
	in.Systems = ParseESSystems(sec["essystems"])
	for _, t := range Lines(sec["tools"]) {
		switch strings.TrimSpace(t) {
		case "ffmpeg":
			in.Ffmpeg = true
		case "ffprobe":
			in.Ffprobe = true
		case "xdotool":
			in.Xdotool = true
		case "rsync":
			in.Rsync = true
		case "flatpak":
			in.Flatpak = true
		}
	}
	ap := strings.SplitN(sec["audio"], "--", 2)
	in.AudioCards = ap[0]
	if len(ap) > 1 {
		in.AudioOut = strings.TrimSpace(ap[1])
	}
	in.Hotkeys = sec["hotkeys"]
	in.ESLog = sec["eslog"]
	in.Dmesg = sec["dmesg"]
	in.Processes = sec["ps"]
	in.Samba = strings.TrimSpace(sec["samba"]) == "yes"
	in.ESApiOpen = strings.Contains(sec["esapi"], "listening")
	in.Timezone = strings.TrimSpace(sec["tz"])
	in.GPU = strings.TrimSpace(sec["gpu"])
	for _, l := range Lines(sec["companion"]) {
		if k, v, ok := strings.Cut(l, "="); ok {
			in.Companion[k] = v
		} else if strings.HasPrefix(l, "svc:") {
			in.Companion[strings.TrimPrefix(l, "svc:")] = "svc"
		}
	}
	return in, nil
}

var reEsSetting = regexp.MustCompile(`<(?:string|bool|int|float)\s+name="([^"]+)"\s+value="([^"]*)"`)

// SplitSections divide un output marcato con righe "@@nome".
func SplitSections(out string) map[string]string {
	sec := map[string]string{}
	cur := ""
	var b strings.Builder
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "@@") {
			if cur != "" {
				sec[cur] = b.String()
			}
			cur = strings.TrimSpace(strings.TrimPrefix(l, "@@"))
			b.Reset()
			continue
		}
		b.WriteString(l)
		b.WriteString("\n")
	}
	if cur != "" {
		sec[cur] = b.String()
	}
	return sec
}

func Lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

var reInputCfg = regexp.MustCompile(`(?s)<inputConfig([^>]*)>(.*?)</inputConfig>`)
var reAttr = regexp.MustCompile(`(\w+)="([^"]*)"`)
var reInput = regexp.MustCompile(`<input\s+([^>]*)/?>`)

func ParseESInput(xml string) []InputConfig {
	var out []InputConfig
	for _, m := range reInputCfg.FindAllStringSubmatch(xml, -1) {
		ic := InputConfig{Inputs: map[string]string{}}
		for _, a := range reAttr.FindAllStringSubmatch(m[1], -1) {
			switch a[1] {
			case "type":
				ic.Type = a[2]
			case "deviceName":
				ic.DeviceName = a[2]
			case "deviceGUID":
				ic.GUID = a[2]
			}
		}
		for _, im := range reInput.FindAllStringSubmatch(m[2], -1) {
			at := map[string]string{}
			for _, a := range reAttr.FindAllStringSubmatch(im[1], -1) {
				at[a[1]] = a[2]
			}
			if at["name"] == "" {
				continue
			}
			v := at["type"] + ":" + at["id"]
			if at["type"] != "button" && at["type"] != "key" {
				v += ":" + at["value"]
			}
			ic.Inputs[at["name"]] = v
		}
		out = append(out, ic)
	}
	return out
}

// ParseBios interpreta l'output di batocera-systems.
func ParseBios(out string) ([]BiosItem, int) {
	var items []BiosItem
	ok := 0
	sys := ""
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") && !strings.HasPrefix(t, "MISSING") && !strings.HasPrefix(t, "INVALID") && !strings.HasPrefix(t, "OK") {
			sys = strings.TrimSuffix(t, ":")
			if strings.HasPrefix(t, ">") {
				sys = strings.TrimSpace(strings.TrimPrefix(t, ">"))
			}
			continue
		}
		f := strings.Fields(t)
		if len(f) < 2 {
			continue
		}
		st := strings.ToUpper(f[0])
		switch st {
		case "MISSING", "INVALID":
			detail := ""
			if len(f) > 2 {
				detail = strings.Join(f[2:], " ")
			}
			items = append(items, BiosItem{System: sys, Path: f[1], Status: st, Detail: detail})
		case "OK":
			ok++
		}
	}
	return items, ok
}

var reSystem = regexp.MustCompile(`(?s)<system>(.*?)</system>`)
var reTag = regexp.MustCompile(`(?s)<(\w+)>(.*?)</\w+>`)
var reEmu = regexp.MustCompile(`(?s)<emulator\s+name="([^"]+)"[^>]*>(.*?)</emulator>`)
var reCore = regexp.MustCompile(`<core([^>]*)>([^<]+)</core>`)

func ParseESSystems(xml string) map[string]ESSystem {
	out := map[string]ESSystem{}
	for _, m := range reSystem.FindAllStringSubmatch(xml, -1) {
		s := ESSystem{Emulators: map[string][]string{}}
		body := m[1]
		for _, t := range reTag.FindAllStringSubmatch(body, -1) {
			switch t[1] {
			case "name":
				s.Name = strings.TrimSpace(t[2])
			case "fullname":
				s.Fullname = strings.TrimSpace(t[2])
			case "path":
				s.Path = strings.TrimSpace(t[2])
			case "extension":
				for _, e := range strings.Fields(t[2]) {
					s.Extensions = append(s.Extensions, strings.ToLower(strings.TrimPrefix(e, ".")))
				}
			}
		}
		for _, em := range reEmu.FindAllStringSubmatch(body, -1) {
			for _, c := range reCore.FindAllStringSubmatch(em[2], -1) {
				core := strings.TrimSpace(c[2])
				s.Emulators[em[1]] = append(s.Emulators[em[1]], core)
				if strings.Contains(c[1], `default="true"`) && s.Default == "" {
					s.Default = em[1] + "/" + core
				}
			}
		}
		if s.Default == "" {
			for e, cs := range s.Emulators {
				if len(cs) > 0 {
					s.Default = e + "/" + cs[0]
					break
				}
			}
		}
		if s.Name != "" {
			out[s.Name] = s
		}
	}
	return out
}
