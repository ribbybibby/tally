package scorecard

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"google.golang.org/api/iterator"
)

// Row is a row in the scorecard table
type Row struct {
	RepoName string
	Score    float64
	Date     civil.Date
}

// Table is a table holding scorecard information
type Table interface {
	SelectWhereRepositoryIn(context.Context, []string) ([]*Row, error)
	Insert(context.Context, *Row) error
	String() string
}

type table struct {
	bq        *bigquery.Client
	projectID string
	dataset   string
	tableName string
}

// NewTable returns a table from the given reference
func NewTable(bq *bigquery.Client, tableRef string) (Table, error) {
	parts := strings.Split(tableRef, ".")
	if len(parts) == 2 {
		projectID := bq.Project()
		if projectID == "" {
			return nil, fmt.Errorf("project id is not provided in reference and it can't be retrieved from the client")
		}
		return &table{
			projectID: projectID,
			dataset:   parts[0],
			tableName: parts[1],
			bq:        bq,
		}, nil
	}
	if len(parts) == 3 {
		return &table{
			projectID: parts[0],
			dataset:   parts[1],
			tableName: parts[2],
			bq:        bq,
		}, nil
	}

	return nil, fmt.Errorf("invalid table reference: %s", tableRef)
}

// String returns a string representation of the table in the form:
// <project-id>.<dataset-name>.<table-name>
func (t *table) String() string {
	return strings.Join([]string{t.projectID, t.dataset, t.tableName}, ".")
}

// Insert a row into the table
func (t *table) Insert(ctx context.Context, row *Row) error {
	u := t.bq.DatasetInProject(t.projectID, t.dataset).Table(t.tableName).Inserter()

	return u.Put(ctx, []*Row{row})
}

// SelectWhereRepositoryIn selects rows for the provided list of repositories
func (t *table) SelectWhereRepositoryIn(ctx context.Context, repos []string) ([]*Row, error) {
	var rows []*Row

	q := t.bq.Query(fmt.Sprintf(`
SELECT repo.name as reponame, score, date
FROM `+fmt.Sprintf("`%s`", t)+`
WHERE repo.name IN ('%s');
`,
		strings.Join(repos, "', '"),
	))

	it, err := q.Read(ctx)
	if err != nil {
		return rows, err
	}

	for {
		row := &Row{}
		err := it.Next(row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return rows, err
		}

		rows = append(rows, row)
	}

	return rows, nil
}
