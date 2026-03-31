package tts

import (
	"testing"
)

func TestResolveVoiceFound(t *testing.T) {
	outputs := []interface{}{
		map[string]interface{}{
			"choices": []interface{}{
				[]interface{}{"SampleVoice", "/path/to/voices/sample.wav"},
				[]interface{}{"OtherVoice", "/path/to/voices/other.wav"},
			},
		},
	}

	got := resolveVoice(outputs, "SampleVoice")
	if got != "/path/to/voices/sample.wav" {
		t.Errorf("resolveVoice = %q, want path to sample", got)
	}
}

func TestResolveVoiceNotFound(t *testing.T) {
	outputs := []interface{}{
		map[string]interface{}{
			"choices": []interface{}{
				[]interface{}{"OtherVoice", "/path/to/voices/other.wav"},
			},
		},
	}

	got := resolveVoice(outputs, "MissingVoice")
	if got != "MissingVoice" {
		t.Errorf("resolveVoice = %q, want fallback to wanted name", got)
	}
}

func TestResolveVoiceEmptyOutputs(t *testing.T) {
	got := resolveVoice(nil, "Test")
	if got != "Test" {
		t.Errorf("resolveVoice(nil) = %q, want %q", got, "Test")
	}
}

func TestResolveFineTunedFound(t *testing.T) {
	outputs := []interface{}{
		map[string]interface{}{
			"choices": []interface{}{
				[]interface{}{"SampleVoice", "sample_model_id"},
				[]interface{}{"Default", "internal"},
			},
		},
	}

	got := resolveFineTuned(outputs, "SampleVoice")
	if got != "sample_model_id" {
		t.Errorf("resolveFineTuned = %q, want %q", got, "sample_model_id")
	}
}

func TestResolveFineTunedEmpty(t *testing.T) {
	got := resolveFineTuned(nil, "")
	if got != "internal" {
		t.Errorf("resolveFineTuned(nil, empty) = %q, want %q", got, "internal")
	}
}

func TestGradioFileData(t *testing.T) {
	fd := gradioFileData("/tmp/test.txt")
	if fd["path"] != "/tmp/test.txt" {
		t.Errorf("path = %v, want /tmp/test.txt", fd["path"])
	}
	meta, ok := fd["meta"].(map[string]string)
	if !ok {
		t.Fatal("meta not a map[string]string")
	}
	if meta["_type"] != "gradio.FileData" {
		t.Errorf("meta._type = %q, want %q", meta["_type"], "gradio.FileData")
	}
}
