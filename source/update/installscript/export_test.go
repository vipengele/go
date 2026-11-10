package installscript

import "go.vipengele.org/update"

// FillTemplate returns the template src filled as Render fills its own, for cfg resolved as Render
// resolves it, with no token but the environment-variable ones.
func FillTemplate(src string, cfg update.Config) ([]byte, error) {
	r, err := resolve(cfg, shGOOS)
	if err != nil {
		return nil, err
	}
	return fill(src, r, nil), nil
}
