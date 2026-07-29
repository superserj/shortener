package shadowed

import (
	"log"
	"os"
)

// panic объявлена в пакете и затеняет встроенную функцию: анализатор опирается
// на вывод типов, поэтому такой вызов в отчёт не попадает.
func panic(msg string) {
	log.Print(msg)
}

// Use вызывает объявленную выше функцию, а не встроенную.
func Use() {
	panic("свой обработчик")
}

type stopper struct{}

// Fatal — метод собственного типа, совпадающий по имени с log.Fatal.
func (stopper) Fatal(msg string) {
	log.Print(msg)
}

// UseMethod проверяет, что анализатор различает функцию пакета log и метод.
func UseMethod() {
	var s stopper
	s.Fatal("остановка")
}

// UseStdLogger вызывает метод (*log.Logger).Fatal: требование инкремента
// сформулировано про функции пакета log, поэтому метод не отмечается.
func UseStdLogger() {
	log.New(os.Stdout, "", 0).Fatal("остановка")
}
