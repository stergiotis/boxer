//go:generate go run github.com/dmarkham/enumer -type=QueryKindE
package clickhouse

type QueryKindE uint8

const (
	QueryKindNone   QueryKindE = 0
	QueryKindSelect QueryKindE = 1
	QueryKindInsert QueryKindE = 2
)
