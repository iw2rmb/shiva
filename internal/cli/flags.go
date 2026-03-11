package cli

type RootFlags struct {
	API        string
	SHA        string
	RevisionID int64
	Profile    string
	Target     string
	Refresh    bool
	Offline    bool
	DryRun     bool
	Output     string
}
