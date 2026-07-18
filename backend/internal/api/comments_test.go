package api

import (
	"testing"
	"time"
)

func TestCommentLimiter(t *testing.T) {
	l := newCommentLimiter(2, time.Minute)
	if !l.allow("wA") || !l.allow("wA") {
		t.Fatal("first two posts by wA should be allowed")
	}
	if l.allow("wA") {
		t.Error("third post by wA within the window should be blocked")
	}
	if !l.allow("wB") {
		t.Error("a different wallet must not be affected by wA's limit")
	}
}

func TestContainsBanned(t *testing.T) {
	if containsBanned("great match, Spain look sharp") {
		t.Error("clean comment flagged")
	}
	if !containsBanned("you RETARD ref") { // case-insensitive substring
		t.Error("banned word not caught")
	}
}
