package analysis

import "strings"

// SecurityReachE says how far a witnessed construct reaches beyond the
// endpoint's own data: the reason it lowered the class to
// [QuerySecurityReadEgress]. Witnesses of a state change carry
// [SecurityReachNone].
type SecurityReachE uint8

const (
	SecurityReachNone SecurityReachE = iota
	// SecurityReachUnknown — a table function this vocabulary does not know;
	// presumed to reach out (fail closed).
	SecurityReachUnknown
	// SecurityReachExternal — external storage or a service: object stores,
	// lake formats, other databases, a URL, an AI provider.
	SecurityReachExternal
	// SecurityReachOtherServers — other ClickHouse servers.
	SecurityReachOtherServers
	// SecurityReachServerFiles — the server's own files, or a program it
	// runs.
	SecurityReachServerFiles
	// SecurityReachRunTime — a query built at run time, whose reach the
	// text does not show.
	SecurityReachRunTime
)

func (inst SecurityReachE) String() (s string) {
	switch inst {
	case SecurityReachNone:
		s = ""
	case SecurityReachExternal:
		s = "external storage or service"
	case SecurityReachOtherServers:
		s = "other servers"
	case SecurityReachServerFiles:
		s = "the server's files or programs"
	case SecurityReachRunTime:
		s = "a query built at run time"
	default:
		s = "unknown table function, presumed to reach out"
	}
	return
}

// tableFunctionInfo is what the classifier knows of a table function: how
// far it reaches (none for a local one), and whether it reads a table
// function among its arguments as a table — `loop(numbers(3))`,
// `remote('h', numbers(10))` — rather than as an expression.
type tableFunctionInfo struct {
	reach      SecurityReachE
	takesTable bool
}

// tableFunctionSpellings is every table function of the server's
// `system.table_functions` (26.8), plus boxer's macros, by how far it
// reaches. A name the server lists and this table does not fails the
// catalog check of the integration lane, so an upgrade that adds a table
// function is classified before it is trusted; meanwhile an unlisted name is
// [SecurityReachUnknown].
var tableFunctionSpellings = map[string]tableFunctionInfo{
	// Row generators and inline data: read nothing but their arguments.
	"numbers": {}, "numbers_mt": {}, "zeros": {}, "zeros_mt": {}, "primes": {},
	"generateRandom": {}, "generate_series": {}, "generateSeries": {},
	"values": {}, "SQLStandardValues": {}, "format": {}, "null": {},
	"fuzzJSON": {}, "fuzzQuery": {},
	// input() reads the data the client sends with the statement.
	"input": {},

	// Server-local objects. What *their* definitions reach — a view over
	// url(), a dictionary with an HTTP source, a table with an S3 engine — is
	// server configuration the text cannot see: the recorded ADR-0132 limit,
	// shared with plain table names.
	"merge": {}, "view": {}, "viewExplain": {}, "dictionary": {},
	"viewIfPermitted": {takesTable: true}, // SELECT … ELSE <table function>
	"loop":            {takesTable: true},
	"mergeTreeIndex":  {}, "mergeTreeProjection": {}, "mergeTreeTextIndex": {},
	"mergeTreeCodecBlockCounts": {}, "mergeTreeAnalyzeIndexes": {}, "mergeTreeAnalyzeIndexesUUID": {},
	"timeSeriesData": {}, "timeSeriesMetrics": {}, "timeSeriesSamples": {},
	"timeSeriesSelector": {}, "timeSeriesTags": {},
	"prometheusQuery": {}, "prometheusQueryRange": {},

	// boxer's macros, classified before expansion (ADR-0132 §SD5): the
	// url() keelson() expands to is pass-generated machinery, not authored
	// egress; docsearch expands to SELECTs over keelson() and
	// system.documentation; the lading macros (ADR-0198 §SD7) to a SELECT
	// over a local MergeTree table, with a mount id and a snapshot as
	// arguments — neither can name a remote.
	"keelson": {}, "docsearch": {}, "fs": {}, "fsdata": {}, "fssnap": {},

	// Other ClickHouse servers.
	"remote":             {reach: SecurityReachOtherServers, takesTable: true},
	"remoteSecure":       {reach: SecurityReachOtherServers, takesTable: true},
	"cluster":            {reach: SecurityReachOtherServers, takesTable: true},
	"clusterAllReplicas": {reach: SecurityReachOtherServers, takesTable: true},

	// The server's files, or a program it runs.
	"file": {reach: SecurityReachServerFiles}, "fileCluster": {reach: SecurityReachServerFiles},
	"filesystem": {reach: SecurityReachServerFiles}, "executable": {reach: SecurityReachServerFiles},
	"sqlite":         {reach: SecurityReachServerFiles},
	"deltaLakeLocal": {reach: SecurityReachServerFiles}, "icebergLocal": {reach: SecurityReachServerFiles},
	"icebergLocalCluster": {reach: SecurityReachServerFiles}, "paimonLocal": {reach: SecurityReachServerFiles},

	// External storage and services.
	"url": {reach: SecurityReachExternal}, "urlCluster": {reach: SecurityReachExternal},
	"s3": {reach: SecurityReachExternal}, "s3Cluster": {reach: SecurityReachExternal},
	"gcs": {reach: SecurityReachExternal}, "cosn": {reach: SecurityReachExternal}, "oss": {reach: SecurityReachExternal},
	"azureBlobStorage": {reach: SecurityReachExternal}, "azureBlobStorageCluster": {reach: SecurityReachExternal},
	"hdfs": {reach: SecurityReachExternal}, "hdfsCluster": {reach: SecurityReachExternal}, "hive": {reach: SecurityReachExternal},
	"iceberg": {reach: SecurityReachExternal}, "icebergCluster": {reach: SecurityReachExternal},
	"icebergS3": {reach: SecurityReachExternal}, "icebergS3Cluster": {reach: SecurityReachExternal},
	"icebergAzure": {reach: SecurityReachExternal}, "icebergAzureCluster": {reach: SecurityReachExternal},
	"icebergHDFS": {reach: SecurityReachExternal}, "icebergHDFSCluster": {reach: SecurityReachExternal},
	"deltaLake": {reach: SecurityReachExternal}, "deltaLakeCluster": {reach: SecurityReachExternal},
	"deltaLakeS3": {reach: SecurityReachExternal}, "deltaLakeS3Cluster": {reach: SecurityReachExternal},
	"deltaLakeAzure": {reach: SecurityReachExternal}, "deltaLakeAzureCluster": {reach: SecurityReachExternal},
	"hudi": {reach: SecurityReachExternal}, "hudiCluster": {reach: SecurityReachExternal},
	"paimon": {reach: SecurityReachExternal}, "paimonCluster": {reach: SecurityReachExternal},
	"paimonS3": {reach: SecurityReachExternal}, "paimonS3Cluster": {reach: SecurityReachExternal},
	"paimonAzure": {reach: SecurityReachExternal}, "paimonAzureCluster": {reach: SecurityReachExternal},
	"paimonHDFS": {reach: SecurityReachExternal}, "paimonHDFSCluster": {reach: SecurityReachExternal},
	"mysql": {reach: SecurityReachExternal}, "postgresql": {reach: SecurityReachExternal},
	"mongodb": {reach: SecurityReachExternal}, "redis": {reach: SecurityReachExternal},
	"jdbc": {reach: SecurityReachExternal}, "odbc": {reach: SecurityReachExternal},
	"bigquery": {reach: SecurityReachExternal}, "arrowFlight": {reach: SecurityReachExternal},
	"ytsaurus": {reach: SecurityReachExternal},

	// eval() runs a query its argument builds at run time.
	"eval": {reach: SecurityReachRunTime},
}

// egressScalarFunctions are the scalar calls that reach beyond the query's
// own data, by how far. The scalar vocabulary is too large to allowlist, so
// an unlisted scalar is presumed pure — the asymmetry ADR-0132 records. The
// catalog check of the integration lane flags a server function whose
// description says it reaches out and that no list here names.
var egressScalarSpellings = map[string]SecurityReachE{
	"file":             SecurityReachServerFiles,
	"catboostEvaluate": SecurityReachServerFiles, // loads a model from the server's files
	// The AI functions send their arguments to the configured provider.
	"aiClassify": SecurityReachExternal, "aiEmbed": SecurityReachExternal,
	"aiExtract": SecurityReachExternal, "aiFilter": SecurityReachExternal,
	"aiGenerate": SecurityReachExternal, "aiRedact": SecurityReachExternal,
	"aiSimilarity": SecurityReachExternal, "aiTranslate": SecurityReachExternal,
}

// stateChangingScalarFunctions change persistent server state from inside a
// SELECT: generateSerialID advances a counter kept in Keeper.
var stateChangingScalarSpellings = map[string]struct{}{
	"generateSerialID": {},
}

// The lookups fold names to lower case: a classification must not depend on
// how the buffer spells a name. TestSecurityVocabularyFolds checks that no
// two spellings fold onto one name.
var (
	tableFunctions               = fold(tableFunctionSpellings)
	egressScalarFunctions        = fold(egressScalarSpellings)
	stateChangingScalarFunctions = fold(stateChangingScalarSpellings)
)

func fold[V any](m map[string]V) (out map[string]V) {
	out = make(map[string]V, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return
}

// lookupTableFunction reports what the vocabulary knows of a table function.
func lookupTableFunction(name string) (info tableFunctionInfo, known bool) {
	info, known = tableFunctions[strings.ToLower(name)]
	return
}
