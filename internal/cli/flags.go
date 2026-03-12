package cli

type RootFlags struct {
	API        string
	SHA        string
	RevisionID int64
	Profile    string
	Target     string
	Offline    bool
	DryRun     bool
	Output     string
	Path       []string
	Query      []string
	Header     []string
	JSON       string
	Body       string
}
