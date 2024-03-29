package command

import (
	"DBMS/storage"
	"DBMS/storage/processors"
	"DBMS/storage/value"
	"DBMS/utils"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
)

type UpdateCommand struct {
	Table  storage.Table
	Values storage.Row
	Where  map[[128]byte]value.Constraint
}

func (c UpdateCommand) Validate() any {
	columns := c.Table.ConvertColumnsToMap()

	for field, value := range c.Values.Entries {
		column := columns[field]

		if column.Autoincrement {
			return errors.New("cannot manually set value of column " + utils.ByteArrayToString(column.Name[:]) + " due to AUTOINCREMENT")
		}

		if column.NotNullable && !column.Autoincrement && value.IsNULL() {
			return errors.New("cannot set value of NOT NULLABLE column " + utils.ByteArrayToString(column.Name[:]) + " to NULL")
		}

		if column.Unique {
			for _, col := range c.Table.Columns {
				if col.Primary {
					if _, exists := c.Where[col.Name]; !exists {
						return errors.New("cannot update value of UNIQUE column " + utils.ByteArrayToString(column.Name[:]) + " without primary key as where constraint, due to multiple possible matches")
					}
				}
			}
		}
	}

	for field, constraint := range c.Where {
		switch columns[field].Type {
		case storage.BOOLEAN:
			if constraint.Operator != value.EQUAL && constraint.Operator != value.NOT_EQUAL {
				return errors.New("invalid binary operator on type BOOLEAN")
			}
		case storage.TEXT:
			if constraint.Operator != value.EQUAL && constraint.Operator != value.NOT_EQUAL {
				return errors.New("invalid binary operator on type TEXT")
			}
		}
	}

	return nil
}

func (c UpdateCommand) Execute() any {
	err := c.Validate()
	if err != nil {
		return err
	}

	idbFile, err := os.OpenFile(c.Table.GetIdbFilePath(), os.O_WRONLY, 0644)
	defer func() {
		idbFile.Close()
		syscall.Flock(int(idbFile.Fd()), syscall.LOCK_UN)
	}()

	err = syscall.Flock(int(idbFile.Fd()), syscall.LOCK_EX)
	if err != nil {
		return err
	}

	columnMap := c.Table.ConvertColumnsToMap()
	where := processors.Where(&c.Table, c.Where)

	rowCount := 0
	yield := make(chan struct{})
	for rowId := range where.Process(yield) {
		for field, fieldValue := range c.Values.Entries {
			column := columnMap[field]

			buffer := bytes.NewBuffer([]byte{})
			binary.Write(buffer, binary.LittleEndian, fieldValue)

			_, err = idbFile.WriteAt(buffer.Bytes(), rowId*c.Table.RowLength+column.Offset)
			if err != nil {
				return err
			}
		}

		rowCount++
	}

	return "CODE 200: updated " + fmt.Sprint(rowCount) + " record(s)"
}
