module unikraft.com/cli

go 1.26.3

tool github.com/caarlos0/svu/v3

require (
	charm.land/bubbles/v2 v2.1.1
	charm.land/bubbletea/v2 v2.0.7
	charm.land/lipgloss/v2 v2.0.6
	github.com/MakeNowJust/heredoc v1.0.0
	github.com/Masterminds/sprig/v3 v3.3.0
	github.com/alecthomas/kong v1.16.1
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de
	github.com/charmbracelet/colorprofile v0.4.3
	github.com/charmbracelet/x/ansi v0.11.8
	github.com/charmbracelet/x/term v0.2.2
	github.com/containerd/containerd/v2 v2.3.4
	github.com/containerd/continuity v0.5.0
	github.com/containerd/errdefs v1.0.0
	github.com/containerd/log v0.1.0
	github.com/containerd/platforms v1.0.0-rc.4
	github.com/cpuguy83/go-md2man/v2 v2.0.7
	github.com/distribution/reference v0.6.0
	github.com/docker/cli v29.7.2+incompatible
	github.com/docker/go-units v0.5.0
	github.com/ettle/strcase v0.2.0
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68
	github.com/google/uuid v1.6.0
	github.com/jotaen/kong-completion v0.0.14
	github.com/juju/errors v1.0.0
	github.com/mitchellh/copystructure v1.2.0
	github.com/moby/buildkit v0.32.2
	github.com/moby/moby/client v0.5.1
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c
	github.com/posener/complete v1.2.3
	github.com/posthog/posthog-go v1.23.1
	github.com/rs/zerolog v1.35.1
	github.com/sergi/go-diff v1.4.0
	github.com/sirupsen/logrus v1.10.0
	github.com/stretchr/testify v1.12.0
	github.com/tidwall/gjson v1.19.0
	github.com/tonistiigi/go-csvvalue v0.0.0-20240814133006-030d3b2625d0
	github.com/unikraft/go-archivefs v0.0.0-20260513151619-de2950d0a646
	github.com/unikraft/go-cpio v0.0.0-20260415131742-4f1984ca41f3
	golang.org/x/mod v0.40.0
	golang.org/x/sync v0.22.0
	gopkg.in/yaml.v3 v3.0.1
	gotest.tools/v3 v3.5.2
	mvdan.cc/sh/v3 v3.13.1
	sigs.k8s.io/yaml v1.6.0
	unikraft.com/cloud/sdk v0.3.1-0.20260817110643-696a81b27ce2
	unikraft.com/x/colors v0.0.0-20260813113709-544c471e0bc9
	unikraft.com/x/filters v0.0.0-20260804153219-d1b47a40e047
	unikraft.com/x/fingerprint v0.0.0-20260126094137-ab6e717e5679
	unikraft.com/x/guesstermwidth v0.0.0-20260813113709-544c471e0bc9
	unikraft.com/x/iata v0.0.0-20260713183529-fd34645687a0
	unikraft.com/x/image-spec v0.0.0-20260813113709-544c471e0bc9
	unikraft.com/x/joinerrgroup v0.0.0-20260304162956-523940cab1de
	unikraft.com/x/kingkong v0.0.0-20260813113709-544c471e0bc9
	unikraft.com/x/kraftfile v0.0.0-20260522114044-e2da24d09716
	unikraft.com/x/log v0.0.0-20260527070319-f904d21cd6ea
	unikraft.com/x/ptr v0.0.0-20260126094137-ab6e717e5679
	unikraft.com/x/version v0.0.0-20260819081122-82fdef94867a
)

require github.com/andybalholm/brotli v1.1.1 // indirect

require (
	charm.land/fang/v2 v2.0.1 // indirect
	dario.cat/mergo v1.0.2 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.5.0 // indirect
	github.com/Microsoft/go-winio v0.6.3-0.20251027160822-ad3df93bed29 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/caarlos0/go-version v0.2.2 // indirect
	github.com/caarlos0/svu/v3 v3.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/harmonica v0.2.0 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260812204455-68fa937c71be // indirect
	github.com/charmbracelet/x/exp/charmtone v0.0.0-20250603201427-c31516f43444 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/containerd/console v1.0.5 // indirect
	github.com/containerd/containerd/api v1.11.1 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/ttrpc v1.2.9 // indirect
	github.com/containerd/typeurl/v2 v2.3.0 // indirect
	github.com/dblohm7/wingoes v0.0.0-20240119213807-a09d6be7affa // indirect
	github.com/denisbrodbeck/machineid v1.0.1 // indirect
	github.com/docker/docker-credential-helpers v0.9.8 // indirect
	github.com/docker/go-connections v0.7.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/gofrs/uuid/v5 v5.4.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/in-toto/attestation v1.2.0 // indirect
	github.com/in-toto/in-toto-golang v0.11.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/locker v1.0.1 // indirect
	github.com/moby/moby/api v1.55.0 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/signal v0.7.1 // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/mango v0.1.0 // indirect
	github.com/muesli/mango-cobra v1.2.0 // indirect
	github.com/muesli/mango-pflag v0.1.0 // indirect
	github.com/muesli/roff v0.1.0 // indirect
	github.com/pelletier/go-toml/v2 v2.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/riywo/loginshell v0.0.0-20200815045211-7d26008be1ab // indirect; indirec fmt
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v5 v5.3.1 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.11.0 // indirect
	github.com/shibumi/go-pathspec v1.3.0 // indirect
	github.com/shirou/gopsutil/v3 v3.24.5 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/tonistiigi/fsutil v0.0.0-20260717003753-6d9dc2ebad62 // indirect
	github.com/tonistiigi/units v0.0.0-20180711220420-6950e57a87ea // indirect
	github.com/tonistiigi/vt100 v0.0.0-20240514184818-90bafcd6abab // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.69.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.69.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go4.org/mem v0.0.0-20240501181205-ae6ca9944745 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/exp v0.0.0-20260603202125-055de637280b // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.82.1 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	tailscale.com v1.94.1 // indirect
)
