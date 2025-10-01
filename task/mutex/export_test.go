package mutex

func (d *Dispatcher) ExtractError() error {
	select {
	case err := <-d.Err():
		return err
	default:
		return nil
	}
}
