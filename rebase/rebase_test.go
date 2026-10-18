package rebase

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/nami"
)

type rec struct {
	fetched, spawned []string
	spawn            bd.Successor
	parent           string
	repo             git.Repo
	headRepo         git.Repo
	order            []string
}

// fake builds scripted seams over a single container.
func fake(c bd.Bead, r *rec) Seams {
	return Seams{
		Show: func(id string) (bd.Bead, error) {
			if id == c.ID {
				return c, nil
			}
			return bd.Bead{}, errors.New("not found")
		},
		Repo:      func(bd.Bead) (git.Repo, string) { return r.repo, "" },
		Strategy:  func() (string, error) { return git.LandStrategyLocal, nil },
		Fetch:     func(git.Repo) error { r.order = append(r.order, "fetch"); return nil },
		HasBranch: func(git.Repo, string) bool { return true },
		Landed:    func(git.Repo, string) (bool, error) { return false, nil },
		Base:      func(git.Repo) (string, error) { r.order = append(r.order, "base"); return "base1", nil },
		Head: func(rp git.Repo, ref string) (string, error) {
			r.headRepo = rp
			return "tip1", nil
		},
		IsAncestor: func(git.Repo, string, string) bool { return false },
		Spawn: func(p string, s bd.Successor) (string, bool, error) {
			r.parent, r.spawn = p, s
			return "mc-new", true, nil
		},
		FindOpenMR: func(git.Repo, string) (*forge.MR, error) { return nil, nil },
	}
}

func open(kind contract.Kind) bd.Bead {
	return bd.Bead{ID: "mc-c", Kind: kind, Title: "My chart", Status: "open"}
}

func TestRequest(t *testing.T) {
	gitlab := func(s *Seams) { s.Strategy = func() (string, error) { return git.LandStrategyGitlab, nil } }
	existing := func(desc string) func(*Seams) {
		return func(s *Seams) {
			s.Spawn = func(string, bd.Successor) (string, bool, error) { return "mc-old", false, nil }
			show := s.Show
			s.Show = func(id string) (bd.Bead, error) {
				if id == "mc-old" {
					return bd.Bead{ID: id, Title: "old", Description: desc}, nil
				}
				return show(id)
			}
		}
	}
	tests := []struct {
		name   string
		bead   bd.Bead
		tweak  func(*Seams)
		want   Result
		reason string
		warn   string
	}{
		{"filed checkpoint", open(contract.KindCheckpoint), nil, Filed, "", ""},
		{"filed chart", open(contract.KindChart), nil, Filed, "", ""},
		{"other kind", open(contract.KindTask), nil, Refused, "task", ""},
		{"closed", bd.Bead{ID: "mc-c", Kind: contract.KindChart, Status: "closed"}, nil, Refused, "closed", ""},
		{"no bead", bd.Bead{ID: "other"}, nil, Refused, "no such bead", ""},
		{"no branch", open(contract.KindChart), func(s *Seams) { s.HasBranch = func(git.Repo, string) bool { return false } }, Refused, "no branch", ""},
		{"landed", open(contract.KindChart), func(s *Seams) { s.Landed = func(git.Repo, string) (bool, error) { return true, nil } }, Refused, "landed", ""},
		{"already on mainline", open(contract.KindChart), func(s *Seams) { s.IsAncestor = func(git.Repo, string, string) bool { return true } }, AlreadyOnMain, "", ""},
		{"already requested", open(contract.KindCheckpoint), existing("Base: b0\nvariant: requested"), AlreadyRequested, "", ""},
		{"open conflict", open(contract.KindCheckpoint), existing("rebase conflict\nBase: b0\nTip: t0"), OpenConflict, "", ""},
		{"mr warning", open(contract.KindChart), func(s *Seams) {
			gitlab(s)
			s.FindOpenMR = func(git.Repo, string) (*forge.MR, error) { return &forge.MR{WebURL: "http://mr/1"}, nil }
		}, Filed, "", "http://mr/1"},
		{"mr lookup fails", open(contract.KindChart), func(s *Seams) {
			gitlab(s)
			s.FindOpenMR = func(git.Repo, string) (*forge.MR, error) { return nil, errors.New("429") }
		}, Filed, "", "could not check for an open MR"},
		{"local strategy skips MR lookup", open(contract.KindChart), func(s *Seams) {
			s.FindOpenMR = func(git.Repo, string) (*forge.MR, error) { t.Error("MR looked up off gitlab"); return nil, nil }
		}, Filed, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &rec{}
			s := fake(open(contract.KindChart), r)
			if tc.bead.ID == "other" {
				s = fake(open(contract.KindChart), r)
				tc.bead.ID = "mc-c"
				s.Show = func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("issue not found") }
			} else {
				s = fake(tc.bead, r)
			}
			if tc.tweak != nil {
				tc.tweak(&s)
			}
			out, err := Request(s, "mc-c")
			if err != nil || out.Result != tc.want {
				t.Fatalf("got %+v, %v; want result %v", out, err, tc.want)
			}
			if !strings.Contains(out.Reason, tc.reason) || !strings.Contains(out.Warning, tc.warn) || (tc.warn != "" && out.Warning == "") {
				t.Errorf("reason %q warning %q, want %q / %q", out.Reason, out.Warning, tc.reason, tc.warn)
			}
			if tc.want == Refused && r.parent != "" {
				t.Error("a refusal filed a bead")
			}
			if tc.name == "open conflict" && out.Base != "b0" {
				t.Errorf("open conflict Base = %q, want b0", out.Base)
			}
		})
	}
}

func TestFiledShape(t *testing.T) {
	r := &rec{repo: "/ws/child"}
	s := fake(open(contract.KindCheckpoint), r)
	s.Strategy = func() (string, error) { return git.LandStrategyGitlab, nil }
	out, err := Request(s, "mc-c")
	if err != nil || out.Result != Filed || out.ID != "mc-new" {
		t.Fatalf("got %+v, %v", out, err)
	}
	if r.parent != "mc-c" || r.spawn.Kind != contract.KindConflict || r.spawn.Title != nami.RequestedCheckpointTitle("mc-c") {
		t.Errorf("spawned %+v under %s", r.spawn, r.parent)
	}
	if b, _ := git.ParseBase(r.spawn.Description); b != "base1" {
		t.Errorf("Base pin = %q", b)
	}
	if tp, _ := git.ParseTip(r.spawn.Description); tp != "tip1" || nami.BodyVariant(r.spawn.Description) != nami.ConflictRequested {
		t.Errorf("body %q", r.spawn.Description)
	}
	if r.headRepo != "/ws/child" {
		t.Errorf("tip read in %q, want the nested child repo", r.headRepo)
	}
	if strings.Join(r.order, ",") != "fetch,base" {
		t.Errorf("order %v: gitlab must fetch before pinning", r.order)
	}

	r = &rec{}
	out, _ = Request(fake(open(contract.KindChart), r), "mc-c")
	if r.spawn.Kind != contract.KindChartConflict || r.spawn.Title != "My chart rebases onto main" || nami.BodyVariant(r.spawn.Description) != nami.ConflictRequested {
		t.Errorf("chart spawn %+v", r.spawn)
	}
}

func TestInfraErrorFilesNothing(t *testing.T) {
	r := &rec{}
	s := fake(open(contract.KindChart), r)
	s.Strategy = func() (string, error) { return git.LandStrategyGitlab, nil }
	s.Fetch = func(git.Repo) error { return errors.New("offline") }
	if _, err := Request(s, "mc-c"); err == nil || r.parent != "" {
		t.Errorf("err %v, parent %q: a failed fetch must error and file nothing", err, r.parent)
	}
}

func TestRequestShowOutageIsAnErrorNotARefusal(t *testing.T) {
	s := fake(bd.Bead{}, &rec{})
	s.Show = func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("dolt unreachable") }
	if out, err := Request(s, "mc-c"); err == nil {
		t.Fatalf("got %+v, nil; want the infrastructure error", out)
	}
}
