package hoist_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHoist(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hoist Suite")
}
