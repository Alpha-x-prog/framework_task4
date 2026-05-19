package main

import "fmt"

// ─── Состояния ───────────────────────────────────────────────────────────────

type State string

const (
	StateNew              State = "Новый"
	StateAppAccepted      State = "ЗаявкаПринята"
	StateResourceBooked   State = "РесурсЗабронирован"
	StateAccessGranted    State = "ДоступВыдан"
	StateCompleted        State = "Завершён"
	StateError            State = "Ошибка"
	StateCompensationDone State = "КомпенсацияВыполнена"
)

// ─── События ─────────────────────────────────────────────────────────────────

type Event string

const (
	EventAcceptApplication Event = "AcceptApplication"
	EventBook              Event = "Book"
	EventGrantAccess       Event = "GrantAccess"
	EventComplete          Event = "Complete"
)

// ─── Процесс ─────────────────────────────────────────────────────────────────

type Process struct {
	Key   string
	State State
}

// ─── Переход ─────────────────────────────────────────────────────────────────

// Transition применяет событие к процессу и возвращает тип записи для лога.
// Если переход недопустим — возвращает ошибку, состояние не меняется.
// При simulate=true вызывает сбой шага: для GrantAccess выполняет компенсацию,
// для остальных переводит в Ошибка.
func Transition(proc *Process, event Event, simulate bool) (logType string, err error) {
	switch event {

	case EventAcceptApplication:
		if proc.State != StateNew {
			return "", fmt.Errorf("нельзя выполнить %s из состояния %s", event, proc.State)
		}
		if simulate {
			proc.State = StateError
		} else {
			proc.State = StateAppAccepted
		}
		return "transition", nil

	case EventBook:
		if proc.State != StateAppAccepted {
			return "", fmt.Errorf("нельзя выполнить %s из состояния %s", event, proc.State)
		}
		if simulate {
			proc.State = StateError
		} else {
			proc.State = StateResourceBooked
		}
		return "transition", nil

	case EventGrantAccess:
		if proc.State != StateResourceBooked {
			return "", fmt.Errorf("нельзя выполнить %s из состояния %s", event, proc.State)
		}
		if simulate {
			// Компенсация: откатываем результат шага Book
			proc.State = StateCompensationDone
			return "compensation", nil
		}
		proc.State = StateAccessGranted
		return "transition", nil

	case EventComplete:
		if proc.State != StateAccessGranted {
			return "", fmt.Errorf("нельзя выполнить %s из состояния %s", event, proc.State)
		}
		if simulate {
			proc.State = StateError
		} else {
			proc.State = StateCompleted
		}
		return "transition", nil

	default:
		return "", fmt.Errorf("неизвестное событие: %s", event)
	}
}
