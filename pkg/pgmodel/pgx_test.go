package pgmodel

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgproto3/v2"
	"github.com/jackc/pgx/v4"
	"github.com/prometheus/prometheus/pkg/labels"
)

type mockPGXConn struct {
	DBName            string
	ExecSQLs          []string
	ExecArgs          [][]interface{}
	ExecErr           error
	QuerySQLs         []string
	QueryArgs         [][]interface{}
	QueryResults      [][]interface{} //Indexed by query number, row number. All columns get the same value
	QueryResultsIndex int
	QueryNoRows       bool
	QueryErr          error
	CopyFromTableName []pgx.Identifier
	CopyFromColumns   [][]string
	CopyFromRowSource []pgx.CopyFromSource
	CopyFromResult    int64
	CopyFromError     error
	CopyFromRowsRows  [][]interface{}
	Batch             []*mockBatch
}

func (m *mockPGXConn) Close(c context.Context) error {
	return nil
}

func (m *mockPGXConn) UseDatabase(dbName string) {
	m.DBName = dbName
}

func (m *mockPGXConn) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	m.ExecSQLs = append(m.ExecSQLs, sql)
	m.ExecArgs = append(m.ExecArgs, arguments)
	return pgconn.CommandTag([]byte{}), m.ExecErr
}

func (m *mockPGXConn) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	defer func() {
		m.QueryResultsIndex++
	}()
	m.QuerySQLs = append(m.QuerySQLs, sql)
	m.QueryArgs = append(m.QueryArgs, args)
	return &mockRows{results: m.QueryResults[m.QueryResultsIndex], noNext: m.QueryNoRows}, m.QueryErr
}

func (m *mockPGXConn) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	m.CopyFromTableName = append(m.CopyFromTableName, tableName)
	m.CopyFromColumns = append(m.CopyFromColumns, columnNames)
	m.CopyFromRowSource = append(m.CopyFromRowSource, rowSrc)
	return m.CopyFromResult, m.CopyFromError
}

func (m *mockPGXConn) CopyFromRows(rows [][]interface{}) pgx.CopyFromSource {
	m.CopyFromRowsRows = rows
	return pgx.CopyFromRows(rows)
}

func (m *mockPGXConn) NewBatch() pgxBatch {
	return &mockBatch{}
}

func (m *mockPGXConn) SendBatch(ctx context.Context, b pgxBatch) (pgx.BatchResults, error) {
	defer func() { m.QueryResultsIndex++ }()
	batch := b.(*mockBatch)
	m.Batch = append(m.Batch, batch)
	return &mockBatchResult{results: m.QueryResults[m.QueryResultsIndex]}, m.QueryErr
}

type batchItem struct {
	query     string
	arguments []interface{}
}

// Batch queries are a way of bundling multiple queries together to avoid
// unnecessary network round trips.
type mockBatch struct {
	items []*batchItem
}

func (b *mockBatch) Queue(query string, arguments ...interface{}) {
	b.items = append(b.items, &batchItem{
		query:     query,
		arguments: arguments,
	})
}

type mockBatchResult struct {
	idx     int
	results []interface{}
}

// Exec reads the results from the next query in the batch as if the query has been sent with Conn.Exec.
func (m *mockBatchResult) Exec() (pgconn.CommandTag, error) {
	panic("not implemented")
}

// Query reads the results from the next query in the batch as if the query has been sent with Conn.Query.
func (m *mockBatchResult) Query() (pgx.Rows, error) {
	panic("not implemented")
}

// Close closes the batch operation. This must be called before the underlying connection can be used again. Any error
// that occurred during a batch operation may have made it impossible to resyncronize the connection with the server.
// In this case the underlying connection will have been closed.
func (m *mockBatchResult) Close() error {
	return nil
}

// QueryRow reads the results from the next query in the batch as if the query has been sent with Conn.QueryRow.
func (m *mockBatchResult) QueryRow() pgx.Row {
	res := []interface{}{
		m.results[m.idx],
	}
	m.idx++
	return &mockRows{results: res, noNext: false}
}

type mockRows struct {
	idx     int
	noNext  bool
	results []interface{}
}

// Close closes the rows, making the connection ready for use again. It is safe
// to call Close after rows is already closed.
func (m *mockRows) Close() {
}

// Err returns any error that occurred while reading.
func (m *mockRows) Err() error {
	panic("not implemented")
}

// CommandTag returns the command tag from this query. It is only available after Rows is closed.
func (m *mockRows) CommandTag() pgconn.CommandTag {
	panic("not implemented")
}

func (m *mockRows) FieldDescriptions() []pgproto3.FieldDescription {
	panic("not implemented")
}

// Next prepares the next row for reading. It returns true if there is another
// row and false if no more rows are available. It automatically closes rows
// when all rows are read.
func (m *mockRows) Next() bool {
	return !m.noNext && m.idx < len(m.results)
}

// Scan reads the values from the current row into dest values positionally.
// dest can include pointers to core types, values implementing the Scanner
// interface, []byte, and nil. []byte will skip the decoding process and directly
// copy the raw bytes received from PostgreSQL. nil will skip the value entirely.
func (m *mockRows) Scan(dest ...interface{}) error {
	if m.idx >= len(m.results) {
		return fmt.Errorf("scanning error")
	}

	switch m.results[m.idx].(type) {
	case int32:
		for i := range dest {
			dv := reflect.ValueOf(dest[i])
			dvp := reflect.Indirect(dv)
			dvp.SetInt(int64(m.results[m.idx].(int32)))
		}
	case uint64:
		for i := range dest {
			dv := reflect.ValueOf(dest[i])
			dvp := reflect.Indirect(dv)
			dvp.SetUint(m.results[m.idx].(uint64))
		}
	case string:
		for i := range dest {
			dv := reflect.ValueOf(dest[i])
			dvp := reflect.Indirect(dv)
			dvp.SetString(m.results[m.idx].(string))
		}
	}

	m.idx++
	return nil
}

// Values returns the decoded row values.
func (m *mockRows) Values() ([]interface{}, error) {
	panic("not implemented")
}

// RawValues returns the unparsed bytes of the row values. The returned [][]byte is only valid until the next Next
// call or the Rows is closed. However, the underlying byte data is safe to retain a reference to and mutate.
func (m *mockRows) RawValues() [][]byte {
	panic("not implemented")
}

func createSeriesResults(x int32) []interface{} {
	ret := make([]interface{}, 0, x)
	var i int32 = 1
	x++

	for i < x {
		ret = append(ret, interface{}(i))
		i++
	}

	return ret
}

func createSeries(x int) []*labels.Labels {
	ret := make([]*labels.Labels, 0, x)
	i := 1
	x++

	for i < x {
		label := labels.Labels{
			labels.Label{
				Name:  fmt.Sprintf("name_%d", i),
				Value: fmt.Sprintf("value_%d", i),
			},
		}
		ret = append(ret, &label)
		i++
	}

	return ret
}

func TestPGXInserterInsertSeries(t *testing.T) {
	testCases := []struct {
		name        string
		series      []*labels.Labels
		queryResult []interface{}
		queryErr    error
		callbackErr error
	}{
		{
			name: "Zero series",
		},
		{
			name:        "One series",
			series:      createSeries(1),
			queryResult: createSeriesResults(1),
		},
		{
			name:        "Two series",
			series:      createSeries(2),
			queryResult: createSeriesResults(2),
		},
		{
			name:        "Double series",
			series:      append(createSeries(2), createSeries(1)...),
			queryResult: createSeriesResults(2),
		},
		{
			name:        "Query err",
			series:      createSeries(2),
			queryResult: createSeriesResults(2),
			queryErr:    fmt.Errorf("some query error"),
		},
		{
			name:        "callback err",
			series:      createSeries(2),
			queryResult: createSeriesResults(2),
			callbackErr: fmt.Errorf("some callback error"),
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			mock := &mockPGXConn{
				QueryErr:     c.queryErr,
				QueryResults: [][]interface{}{c.queryResult},
			}
			inserter := pgxInserter{conn: mock}

			var newSeries []SeriesWithCallback

			calls := 0
			for _, ser := range c.series {
				newSeries = append(newSeries, SeriesWithCallback{
					Series: *ser,
					Callback: func(l labels.Labels, id SeriesID) error {
						calls++
						return c.callbackErr
					},
				})
			}

			err := inserter.InsertSeries(newSeries)

			if err != nil {
				if c.queryErr != nil {
					if err != c.queryErr {
						t.Errorf("unexpected query error:\ngot\n%s\nwanted\n%s", err, c.queryErr)
					}
					return
				}
				if c.callbackErr != nil {
					if err != c.callbackErr {
						t.Errorf("unexpected callback error:\ngot\n%s\nwanted\n%s", err, c.callbackErr)
					}
					return
				}
				t.Errorf("unexpected error: %v", err)
			}

			if calls != len(c.series) {
				t.Errorf("Callback called wrong number of times: got %v expected %c", calls, len(c.series))
			}

			if c.queryErr != nil {
				t.Errorf("expected query error:\ngot\n%v\nwanted\n%v", err, c.queryErr)
			}
		})
	}
}

func createRows(x int) map[string]*SampleInfoIterator {
	return createRowsByMetric(x, 1)
}

func createRowsByMetric(x int, metricCount int) map[string]*SampleInfoIterator {
	ret := make(map[string]*SampleInfoIterator, 0)
	i := 0

	metrics := make([]string, 0, metricCount)

	for metricCount > i {
		metrics = append(metrics, fmt.Sprintf("metric_%d", i))
		i++
	}

	i = 0

	for i < x {
		metricIndex := i % metricCount
		if _, ok := ret[metrics[metricIndex]]; !ok {
			ret[metrics[metricIndex]] = NewSampleInfoIterator()
		}
		ret[metrics[metricIndex]].Append(&samplesInfo{})
		i++
	}
	return ret
}

func TestPGXInserterInsertData(t *testing.T) {
	testCases := []struct {
		name           string
		rows           map[string]*SampleInfoIterator
		queryNoRows    bool
		queryErr       error
		copyFromResult int64
		copyFromErr    error
	}{
		{
			name: "Zero data",
		},
		{
			name: "One data",
			rows: createRows(1),
		},
		{
			name: "Two data",
			rows: createRows(2),
		},
		{
			name: "Two metrics",
			rows: createRowsByMetric(2, 2),
		},
		{
			name:     "Create table error",
			rows:     createRows(5),
			queryErr: fmt.Errorf("create table error"),
		},
		{
			name:        "Copy from error",
			rows:        createRows(5),
			copyFromErr: fmt.Errorf("some error"),
		},
		{
			name:           "Not all data inserted",
			rows:           createRows(5),
			copyFromResult: 4,
		},
		{
			name:        "Can't find/create table in DB",
			rows:        createRows(5),
			queryNoRows: true,
		},
	}
	for _, co := range testCases {
		c := co
		t.Run(c.name, func(t *testing.T) {
			mock := &mockPGXConn{
				QueryNoRows:    c.queryNoRows,
				QueryErr:       c.queryErr,
				CopyFromResult: c.copyFromResult,
				CopyFromError:  c.copyFromErr,
			}

			//The database will be queried for metricNames
			results := [][]interface{}{}
			for metricName := range c.rows {
				results = append(results, []interface{}{metricName})

			}
			mock.QueryResults = results

			inserter := pgxInserter{conn: mock}

			inserted, err := inserter.InsertData(c.rows)

			if err != nil {
				if c.copyFromErr != nil {
					if err != c.copyFromErr {
						t.Errorf("unexpected error:\ngot\n%s\nwanted\n%s", err, c.copyFromErr)
					}
					return
				}
				if c.queryErr != nil {
					if err != c.queryErr {
						t.Errorf("unexpected error:\ngot\n%s\nwanted\n%s", err, c.copyFromErr)
					}
					return
				}
				if c.copyFromResult == int64(len(c.rows)) {
					t.Errorf("unexpected error:\ngot\n%s\nwanted\nnil", err)
				}
				if err == errMissingTableName {
					if !c.queryNoRows {
						t.Errorf("got missing table name error but query returned a result")
					}
					return
				}
				t.Errorf("Unexpected error %v", err)
				return
			}

			if c.copyFromErr != nil {
				t.Errorf("expected error:\ngot\nnil\nwanted\n%s", c.copyFromErr)
			}

			if inserted != uint64(c.copyFromResult) {
				t.Errorf("unexpected number of inserted rows:\ngot\n%d\nwanted\n%d", inserted, c.copyFromResult)
			}

			if len(c.rows) == 0 {
				return
			}

			if len(mock.CopyFromTableName) != len(c.rows) {
				t.Errorf("number of table names differs from input: got %d want %d\n", len(mock.CopyFromTableName), len(c.rows))
			}
			tNames := make([]pgx.Identifier, 0, len(c.rows))

			for tableName := range c.rows {
				tNames = append(tNames, pgx.Identifier{dataTableSchema, tableName})
			}
			if !reflect.DeepEqual(mock.CopyFromTableName, tNames) {
				t.Errorf("unexpected copy table:\ngot\n%s\nwanted\n%s", mock.CopyFromTableName, tNames)
			}

			for _, cols := range mock.CopyFromColumns {
				if !reflect.DeepEqual(cols, copyColumns) {
					t.Errorf("unexpected columns:\ngot\n%s\nwanted\n%s", cols, copyColumns)
				}
			}

			for i, metric := range mock.CopyFromTableName {
				name := metric[1]
				result := c.rows[name]

				if !reflect.DeepEqual(mock.CopyFromRowSource[i], result) {
					t.Errorf("unexpected rows:\ngot\n%+v\nwanted\n%+v", mock.CopyFromRowSource[i], result)
				}
			}

		})
	}
}