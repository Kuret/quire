package prober

import "testing"

// The verdict booleans, asked directly.
//
// stageCapability ends the stage the moment the strong check fails, so on the
// live path a failed Confirm can only ever be seen alongside a Search that was
// never run. That makes the guards in ok() and addableDegraded() unreachable
// from outside — and unreachable is exactly the state in which a guard rots.
// Here they are asked the question they exist to answer.
func TestAFailedStrongCheckIsNeverAddable(t *testing.T) {
	// Everything a working source has, except that the site is not the
	// application it claimed to be.
	c := capability{
		Confirm:  stepResult{Note: "it is missing books_output_mode."},
		Search:   stepResult{OK: true, Count: 40},
		Series:   stepResult{OK: true},
		Chapters: stepResult{OK: true, Count: 34},
		Pages:    stepResult{OK: true, Count: 12},
		Image:    stepResult{OK: true, Count: 900},
	}

	if c.ok() {
		t.Error("a source that failed the strong check was reported as working")
	}
	// The degraded allowance is for a source that is what it says it is and
	// does part of the job. A site that is not the application is not a weaker
	// version of it.
	if c.addableDegraded() {
		t.Error("a source that failed the strong check was offered in a degraded state")
	}
	if got := c.failure(); got == "" {
		t.Error("the failure names no step, so the user is told nothing")
	}
}

// The file shape of the same question: a release step that failed is as fatal
// as a page image that could not be fetched, and a page step that was never run
// must not be read as a failure.
func TestFileCapabilityIgnoresThePageSteps(t *testing.T) {
	working := capability{
		fileBased: true,
		Confirm:   stepResult{OK: true},
		Search:    stepResult{OK: true, Count: 40},
		Series:    stepResult{OK: true},
		Chapters:  stepResult{OK: true, Count: 34},
		Release:   stepResult{OK: true, Count: 34},
		// Pages and Image are deliberately zero: a file theme has none, and
		// their absence must not be read as their failure.
	}
	if !working.ok() {
		t.Errorf("a working book source was refused: %s", working.failure())
	}

	broken := working
	broken.Release = stepResult{Note: "nothing to ask for."}
	if broken.ok() || broken.addableDegraded() {
		t.Error("a book source with nothing to fetch was accepted")
	}
}
