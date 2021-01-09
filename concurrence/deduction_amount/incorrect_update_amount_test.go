package goclub_book_concurrence_deduction_amount

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"log"
	"sync"
	"testing"
)

var db *sqlx.DB
func init() {
	var err error
	db , err = sqlx.Open("mysql", "root:somepass@(localhost:3306)/goclub_book") ; if err != nil {
		panic(err)
	}
}



func QueryAmountByName(name string) (amount float64) {
	queryAmountSQL := "SELECT `amount` FROM `deduction_amount` WHERE `name` = ?"
	row := db.QueryRowx(queryAmountSQL, name)
	var has bool
	err := row.Scan(&amount) ; if err != nil {
		if err == sql.ErrNoRows {
			has = false
		} else {
			panic(err)
		}
	} else {
		has = true
	}
	if !has {
		panic(errors.New("not found data"))
	}
	return amount
}
func ResetAmountByName(name string, amount float64) {
	queryAmountSQL := "SELECT `id` FROM `deduction_amount` WHERE `name` = ?"
	row := db.QueryRowx(queryAmountSQL, name)
	var id int
	scanErr := row.Scan(&id)
	has, err := CheckScanError(scanErr) ; if err != nil {
		panic(err)
	}
	if has {
		updateAmountSQL := "UPDATE `deduction_amount` SET `amount` = ? WHERE `name` = ?"
		_, err := db.Exec(updateAmountSQL, amount, name) ; if err != nil {
			panic(err)
		}
	} else {
		insertAmountSQL := "INSERT INTO `deduction_amount` (`name`, `amount`) VALUES (?, ?)"
		_, err := db.Exec(insertAmountSQL, name, amount) ; if err != nil {
			panic(err)
		}
	}
	return
}

func CheckScanError(scanErr error)(bool, error) {
	if scanErr == nil {
		return true ,scanErr
	}
	if scanErr == sql.ErrNoRows {
		return false, nil
	}
	return false, scanErr
}

func TestIncorrectUpdateSetFixedAmount(t *testing.T) {
	wg := sync.WaitGroup{}
	name := "IncorrectUpdateSetFixedAmount"
	ResetAmountByName(name, 100)
	for i:=0;i<10;i++ {
		wg.Add(1)
		// 必须使用 routine 才能模拟并发
		go func() {
			amount := QueryAmountByName(name)
			updateAmount := amount - 1
			if updateAmount < 0 {
				log.Print("余额不够")
				return
			}
			resetAmountSQL := "UPDATE `deduction_amount` SET `amount` = ? WHERE `name` = ?"
			_, err := db.Exec(resetAmountSQL, updateAmount, name) ; if err != nil { panic(err) }
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestIncorrectSubtractAmountOutOfRange(t *testing.T) {
	name := "IncorrectSubtractAmountOutOfRange"
	wg := sync.WaitGroup{}
	ResetAmountByName(name, 5)
	// 运行会出现 Out of range value for column 'amount' at row 1
	for i:=0;i<10;i++ {
		wg.Add(1)
		go func() {
			resetAmountSQL := "UPDATE `deduction_amount` SET `amount` = `amount` - ?   WHERE `name` = ?"
			_, err := db.Exec(resetAmountSQL, 1, name) ; if err != nil { panic(err) }
			wg.Done()
		}()
	}
	wg.Wait()
}

// 通过 Where 增加修改条件，并检查修改结果，但是 RowsAffected 可能不被支持
func TestIncorrectSubtractAmountSafeRange(t *testing.T) {
	name := "IncorrectSubtractAmountSafeRange"
	wg := sync.WaitGroup{}
	ResetAmountByName(name, 5)
	subtractAmount := 1
	for i:=0;i<10;i++ {
		wg.Add(1)
		go func() {
			resetAmountSQL := "UPDATE `deduction_amount` SET `amount` = `amount` - ? WHERE `amount` - ? >= 0 AND `name` = ?"
			result, err := db.Exec(resetAmountSQL, subtractAmount, subtractAmount, name) ; if err != nil {
				panic(err)
			}
			// RowsAffected returns the number of rows affected by an
			// update, insert, or delete. Not every database or database
			// driver may support this.
			affected, err :=result.RowsAffected() ; if err != nil {
				panic(err)
			}
			if affected == 0 {
				log.Print("数据没有被修改")
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestIncorrectUpdateTransaction(t *testing.T) {
	name := "IncorrectUpdateTransaction"
	wg := sync.WaitGroup{}
	ResetAmountByName(name, 5)
	var subtractAmount float64 = 1
	for i:=0;i<10;i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.BeginTxx(context.TODO(), &sql.TxOptions{sql.LevelRepeatableRead, false}) ; if err != nil {
				panic(err)
			}
			// 使用主键 where 条件能控制锁影响行最小
			queryAmountLockUpdateSQL := "SELECT `id`, `amount` FROM `deduction_amount` WHERE id = ? FOR UPDATE"
			row := tx.QueryRow(queryAmountLockUpdateSQL, 4)
			var id int
			var amount float64
			err = row.Scan(&id, &amount)
			has, err := CheckScanError(err) ; if err != nil {
				tx.Rollback()
				panic(err)
			}
			if !has {panic(errors.New("没有数据"))}
			updatedAmount := amount - subtractAmount
			if updatedAmount < 0 {
				log.Print("余额不够修改失败")
				tx.Rollback()
				return
			}
			// 保持惯例，在任何场景涉及到扣除数字的都使用 field = field - ? 的方式
			_, err = tx.Exec("UPDATE `deduction_amount` SET `amount` = `amount` - ? WHERE `id` = ?", subtractAmount, 4) ; if err != nil {
				tx.Rollback()
				panic(err)
			}
			log.Print("修改成功:", amount)
			tx.Commit()
		}()
	}
	wg.Wait()
}

