package testing

func Test_B_RunParallel_Unsupported(t *T) {
	expectPanic(t, "testing: B.RunParallel is unsupported in Gno", func() {
		b := &B{}
		b.RunParallel(nil)
	})
}

func Test_B_SetParallelism_Unsupported(t *T) {
	expectPanic(t, "testing: B.SetParallelism is unsupported in Gno", func() {
		b := &B{}
		b.SetParallelism(1)
	})
}

func Test_B_Run_Unsupported(t *T) {
	expectPanic(t, "testing: B.Run is not supported", func() {
		b := &B{}
		b.Run("sub", func(b *B) {})
	})
}

func Test_B_Setenv_Unsupported(t *T) {
	expectPanic(t, "testing: B.Setenv is not supported", func() {
		b := &B{}
		b.Setenv("FOO", "BAR")
	})
}

func Test_B_Cleanup_Unsupported(t *T) {
	expectPanic(t, "testing: B.Cleanup is not supported", func() {
		b := &B{}
		b.Cleanup(func() {})
	})
}

func Test_B_ReportMetric_Unsupported(t *T) {
	expectPanic(t, "testing: B.ReportMetric is not supported", func() {
		b := &B{}
		b.ReportMetric(1.0, "x")
	})
}

func Test_B_TempDir_Unsupported(t *T) {
	expectPanic(t, "testing: B.TempDir is not supported", func() {
		b := &B{}
		b.TempDir()
	})
}

func Test_PB_Next_Unsupported(t *T) {
	expectPanic(t, "testing: PB.Next is unsupported in Gno", func() {
		pb := &PB{}
		pb.Next()
	})
}

func expectPanic(t *T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("expected panic %q, got none", want)
			return
		}
		msg, ok := r.(string)
		if !ok {
			t.Errorf("expected string panic %q, got %T: %v", want, r, r)
			return
		}
		if msg != want {
			t.Errorf("panic message mismatch:\n  got:  %q\n  want: %q", msg, want)
		}
	}()
	fn()
}
