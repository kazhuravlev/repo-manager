package repomgr

//go:generate options-gen -from-struct=Options
type Options struct {
	spec       Spec   `option:"mandatory" validate:"required"`
	privateKey string `validate:"required" default:"$HOME/.ssh/id_rsa"`
}
