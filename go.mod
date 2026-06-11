module github.com/stergiotis/boxer

go 1.26.0

toolchain go1.26.4

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/Jeffail/checkpoint v1.1.0
	github.com/Jeffail/shutdown v1.1.0
	github.com/RoaringBitmap/roaring v1.9.4
	github.com/akrylysov/pogreb v0.10.2
	github.com/antlr4-go/antlr/v4 v4.13.1
	github.com/apache/arrow-go/v18 v18.6.0
	github.com/brianvoe/gofakeit/v7 v7.15.0
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/cockroachdb/pebble v1.1.5
	github.com/dgraph-io/badger/v4 v4.9.1
	github.com/dim13/colormap v1.1.0
	github.com/dustin/go-humanize v1.0.1
	github.com/ebitengine/purego v0.10.1
	github.com/ettle/strcase v0.2.0
	github.com/fogleman/gg v1.3.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/fxamacker/cbor/v2 v2.9.2
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433
	github.com/go-text/typesetting v0.3.4
	github.com/goccy/go-graphviz v0.2.10
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0
	github.com/golangci/gofmt v0.0.0-20251215234548-e7be49a5ab4d
	github.com/google/uuid v1.6.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/hishamk/statetrooper v0.1.4
	github.com/klauspost/compress v1.18.6
	github.com/matoous/go-nanoid/v2 v2.1.0
	github.com/mattn/go-isatty v0.0.22
	github.com/nao1215/markdown v0.13.0
	github.com/nozzle/umap-go v0.0.0-20260301204052-79bd84384eff
	github.com/pkoukk/tiktoken-go v0.1.8
	github.com/rs/zerolog v1.35.1
	github.com/sirkon/dst v0.26.4
	github.com/stoewer/go-strcase v1.3.1
	github.com/stretchr/testify v1.11.1
	github.com/testcontainers/testcontainers-go/modules/redpanda v0.42.0
	github.com/tetratelabs/wazero v1.11.0
	github.com/twmb/franz-go v1.21.2
	github.com/twmb/franz-go/pkg/kadm v1.18.0
	github.com/urfave/cli/v2 v2.27.7
	github.com/valyala/bytebufferpool v1.0.0
	github.com/valyala/fasttemplate v1.2.2
	github.com/yassinebenaid/godump v0.11.1
	github.com/yuin/goldmark v1.8.2
	github.com/yuin/goldmark-meta v1.1.0
	github.com/zeebo/xxh3 v1.1.0
	golang.org/x/exp v0.0.0-20260410095643-746e56fc9e2f
	golang.org/x/image v0.41.0
	golang.org/x/sys v0.45.0
	golang.org/x/term v0.43.0
	golang.org/x/tools v0.45.0
	gonum.org/v1/gonum v0.17.0
	gopkg.in/yaml.v3 v3.0.1
	lukechampine.com/blake3 v1.4.1
	pgregory.net/rapid v1.2.0
)

require github.com/dlclark/regexp2 v1.11.5 // indirect

require (
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/CycloneDX/cyclonedx-go v0.9.3 // indirect
	github.com/CycloneDX/cyclonedx-gomod v1.10.0 // indirect
	github.com/DataDog/zstd v1.5.7 // indirect
	github.com/KimMachineGun/automemlimit v0.7.5 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/agnivade/levenshtein v1.2.2-0.20250519083737-420867539855 // indirect
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/apache/thrift v0.23.0 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.22.0 // indirect
	github.com/boyter/gocodewalker v1.5.2-0.20260227212453-19676720409f // indirect
	github.com/boyter/scc/v3 v3.7.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.3.1 // indirect
	github.com/charmbracelet/lipgloss v1.1.0 // indirect
	github.com/charmbracelet/x/ansi v0.9.2 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13 // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/cockroachdb/errors v1.12.0 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240816210425-c5d0cb0b6fc0 // indirect
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506 // indirect
	github.com/cockroachdb/redact v1.1.8 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20250429170803-42689b6311bb // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/danwakefield/fnmatch v0.0.0-20160403171240-cbb64ac3d964 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgraph-io/ristretto/v2 v2.4.0 // indirect
	github.com/dgryski/go-minhash v0.0.0-20170608043002-7fe510aff544 // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dkorunic/betteralign v0.8.0 // indirect
	github.com/docker/go-connections v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ekzhu/minhash-lsh v0.0.0-20171225071031-5c06ee8586a1 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/flopp/go-findfont v0.1.0 // indirect
	github.com/getsentry/sentry-go v0.44.1 // indirect
	github.com/go-enry/go-license-detector/v4 v4.3.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.9.0 // indirect
	github.com/go-git/go-git/v5 v5.16.2 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/renameio/v2 v2.0.0 // indirect
	github.com/hhatto/gorst v0.0.0-20181029133204-ca9f730cac5b // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/incu6us/goimports-reviser/v3 v3.10.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/jdkato/prose v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/karrick/godirwalk v1.17.0 // indirect
	github.com/kevinburke/ssh_config v1.6.0 // indirect
	github.com/kisielk/errcheck v1.10.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-runewidth v0.0.21 // indirect
	github.com/mfridman/tparse v0.18.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.2.0 // indirect
	github.com/moby/moby/api v1.54.1 // indirect
	github.com/moby/moby/client v0.4.0 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/montanaflynn/stats v0.0.0-20171201202039-1bf9dbcd8cbe // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/olekukonko/cat v0.0.0-20250911104152-50322a0618f6 // indirect
	github.com/olekukonko/errors v1.2.0 // indirect
	github.com/olekukonko/ll v0.1.8 // indirect
	github.com/olekukonko/tablewriter v1.1.4 // indirect
	github.com/onsi/gomega v1.38.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/peterbourgon/ff/v3 v3.4.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/pjbgf/sha1cd v0.5.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/shirou/gopsutil/v4 v4.26.3 // indirect
	github.com/shogo82148/go-shuffle v0.0.0-20170808115208-59829097ff3b // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/skeema/knownhosts v1.3.1 // indirect
	github.com/spf13/cobra v1.10.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/testcontainers/testcontainers-go v0.42.0 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/xrash/smetrics v0.0.0-20250705151800-55b8f293f342 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.60.0 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/nilaway v0.0.0-20260528182042-490362de4fb6 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20250210185358-939b2ce775ac // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/telemetry v0.0.0-20260508192327-42602be52be6 // indirect
	golang.org/x/text v0.37.0 // indirect
	golang.org/x/vuln v1.3.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260316180232-0b37fe3546d5 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/neurosnap/sentences.v1 v1.0.6 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	honnef.co/go/tools v0.7.0 // indirect
)

tool (
	github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod
	github.com/boyter/scc/v3
	github.com/dkorunic/betteralign/cmd/betteralign
	github.com/incu6us/goimports-reviser/v3
	github.com/kisielk/errcheck
	github.com/mfridman/tparse
	go.uber.org/nilaway/cmd/nilaway
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)
