package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scripted devolve uma Analysis por chamada de AnalyzeAudio, na ordem.
type scripted struct {
	audio       []Analysis
	audioErrAt  int // 1-based; 0 = nunca
	calls       int
	gotTranscr  string
	transcrFail bool
}

func (s *scripted) Enabled() bool { return true }
func (s *scripted) AnalyzeAudio(context.Context, []byte, string) (Analysis, error) {
	s.calls++
	if s.calls == s.audioErrAt {
		return Analysis{}, errors.New("falhou")
	}
	return s.audio[s.calls-1], nil
}
func (s *scripted) AnalyzeText(context.Context, string) (Analysis, error) { return Analysis{}, nil }
func (s *scripted) AnalyzeTranscript(_ context.Context, t string) (Analysis, error) {
	s.gotTranscr = t
	if s.transcrFail {
		return Analysis{}, errors.New("falhou")
	}
	return Analysis{Summary: "resumo geral", Tags: []string{"a", "b"}}, nil
}
func (s *scripted) Embed(context.Context, string, EmbedTask) ([]float32, error) { return nil, nil }

func seg(n int) [][]byte { return make([][]byte, n) }

func TestSegmentsSingleUsesAudioAnalysisDirectly(t *testing.T) {
	c := &scripted{audio: []Analysis{{HasSpeech: true, Transcript: "um dois três", Summary: "s", Tags: []string{"t"}}}}
	a, err := AnalyzeSegments(context.Background(), c, seg(1), "audio/mp3")
	if err != nil || a.Summary != "s" || c.gotTranscr != "" {
		t.Fatalf("%+v %v (não deveria chamar AnalyzeTranscript)", a, err)
	}
}

func TestSegmentsJoinInOrderAndSummarizeOnce(t *testing.T) {
	c := &scripted{audio: []Analysis{
		{HasSpeech: true, Transcript: "primeiro trecho aqui"},
		{HasSpeech: false}, // só música: fica de fora
		{HasSpeech: true, Transcript: "terceiro trecho aqui"},
	}}
	a, err := AnalyzeSegments(context.Background(), c, seg(3), "audio/mp3")
	if err != nil {
		t.Fatal(err)
	}
	if a.Transcript != "primeiro trecho aqui terceiro trecho aqui" || a.Summary != "resumo geral" || len(a.Tags) != 2 || !a.HasSpeech {
		t.Fatalf("%+v", a)
	}
	if c.gotTranscr != a.Transcript {
		t.Fatalf("resumo deve sair da transcrição completa: %q", c.gotTranscr)
	}
}

func TestSegmentsNoSpeechAnywhere(t *testing.T) {
	c := &scripted{audio: []Analysis{{}, {}}}
	a, err := AnalyzeSegments(context.Background(), c, seg(2), "audio/mp3")
	if err != nil || a.HasSpeech || c.gotTranscr != "" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestSegmentsAnyFailureFailsWholeTranscription(t *testing.T) {
	// Transcrição parcial não pode ser salva como se fosse completa.
	c := &scripted{audio: make([]Analysis, 3), audioErrAt: 2}
	if _, err := AnalyzeSegments(context.Background(), c, seg(3), "audio/mp3"); err == nil || !strings.Contains(err.Error(), "trecho 2 de 3") {
		t.Fatal(err)
	}
	c = &scripted{audio: []Analysis{{HasSpeech: true, Transcript: "a b c"}, {HasSpeech: true, Transcript: "d e f"}}, transcrFail: true}
	if _, err := AnalyzeSegments(context.Background(), c, seg(2), "audio/mp3"); err == nil {
		t.Fatal("esperava erro no resumo")
	}
}
