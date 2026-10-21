package downloader

import "fmt"

func (d *Downloader) LoadCredentials() ([]byte, error) {
	if d.queue.store == nil {
		return nil, fmt.Errorf("persistent credential store unavailable")
	}
	return d.queue.store.LoadCredentials()
}
func (d *Downloader) SaveCredentials(body []byte) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent credential store unavailable")
	}
	return d.queue.store.SaveCredentials(body)
}
func (d *Downloader) BindAccount(id string) error {
	if d.queue.store == nil {
		return fmt.Errorf("persistent account store unavailable")
	}
	return d.queue.store.BindAccount(id)
}

func (d *Downloader) AdministratorKey(configured string) (string, error) {
	if d.queue.store == nil {
		return "", fmt.Errorf("persistent administrator store unavailable")
	}
	return d.queue.store.AdministratorKey(configured)
}
