package database

import (
    "database/sql"
)

func scanPtrFloat(rows *sql.Rows, idx int) (*float64, error) {
    var v sql.NullFloat64
    if err := rows.Scan(&v); err != nil {
        return nil, err
    }
    if v.Valid {
        return &v.Float64, nil
    }
    return nil, nil
}

