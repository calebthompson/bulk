package bulk

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
)

// Insert represents a bulk insert statement. It is initialized using a *sql.DB,
// a SQL string, and an array of cast types for the columns in the insert
// statement. Any of the Casts may be an empty string (""), but the length of
// Casts must be equal to the number of columns being inserted.
// The substring "<values>" in SQL will be replaces with an expression for the
// values being inserted.
type Insert struct {
	*sql.DB
	SQL   string
	Casts []string

	stmt     *sql.Stmt
	bindvars []driver.Value
	result   result
}

func NewInsert(db *sql.DB, sql string, casts []string) *Insert {
	return &Insert{db, sql, casts, nil, nil, result{}}
}

const MaxBindVars = 65535

// Exec runs the Insert statement in as many batches as required to allow
// Insert.DB to fill placeholder vars. The number of batches which will be run
// is equal to len(casts) * len(rows) / MaxBindVars + 1. It returns sql.Result
// and the first error, if any, which occurs will short-circuit the
// operation.
func (s *Insert) Exec(rows [][]driver.Value) (sql.Result, error) {
	var (
		leftovers int
		err       error
	)

	batches := len(s.Casts) * len(rows) / MaxBindVars
	if batches > 0 {
		batchSize := len(rows) / (batches + 1)
		leftovers = len(rows) - batchSize*batches
		for leftovers > batchSize {
			batches++
			leftovers -= batchSize
		}
		err = func() error {
			err := s.prepare(batchSize)
			if err != nil {
				return err
			}
			defer s.stmt.Close()
			for i := 0; i < batches; i++ {
				args := []interface{}{}
				for _, row := range rows[i*batchSize : i*batchSize+batchSize] {
					for _, arg := range row {
						args = append(args, arg)
					}
				}
				res, err := s.stmt.Exec(args...)
				if err != nil {
					return err
				}
				s.result.add(res)
			}
			return nil
		}()
		if err != nil {
			return s.result, err
		}
	} else {
		leftovers = len(rows)
	}

	err = s.prepare(leftovers)
	if err != nil {
		return s.result, err
	}
	defer s.stmt.Close()
	args := []interface{}{}
	for _, row := range rows[len(rows)-leftovers:] {
		for _, arg := range row {
			args = append(args, arg)
		}
	}
	res, err := s.stmt.Exec(args...)
	if err != nil {
		return s.result, err
	}
	s.result.add(res)
	return s.result, nil
}

func (s *Insert) prepare(count int) error {
	var err error
	s.stmt, err = s.Prepare(strings.Replace(s.SQL, "<values>", s.valuePlaceholders(count), 1))
	return err
}

func (s Insert) valuePlaceholders(count int) string {
	values := []string{}
	for i := 0; i < count; i++ {
		val := []string{}
		for j, cast := range s.Casts {
			placeCount := i*len(s.Casts) + j + 1
			if cast == "" {
				val = append(val, fmt.Sprintf("$%d", placeCount))
			} else {
				val = append(val, fmt.Sprintf("$%d::%s", placeCount, cast))
			}
		}
		values = append(values, fmt.Sprintf("(%s)", strings.Join(val, ", ")))
	}
	return strings.Join(values, ",\n")
}
