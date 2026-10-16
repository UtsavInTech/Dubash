package asr

import "context"

// DefaultLines are the scripted captions Fake emits per language.
var DefaultLines = map[string][]string{
	"en": {"Welcome to Nerdearla.", "Today we will talk about open source.", "These captions are generated live."},
	"es": {"Bienvenidos a Nerdearla.", "Hoy vamos a hablar de código abierto.", "Estos subtítulos se generan en vivo."},
	"pt": {"Bem-vindos à Nerdearla.", "Hoje vamos falar de código aberto.", "Estas legendas são geradas ao vivo."},
	"fr": {"Bienvenue à Nerdearla.", "Aujourd'hui, nous allons parler d'open source.", "Ces sous-titres sont générés en direct."},
	"de": {"Willkommen bei Nerdearla.", "Heute sprechen wir über Open Source.", "Diese Untertitel entstehen live."},
	"it": {"Benvenuti a Nerdearla.", "Oggi parliamo di open source.", "Questi sottotitoli sono generati dal vivo."},
	"hi": {"नमस्ते, कार्यक्रम में आपका स्वागत है।", "आज हम अपनी भाषा में सुनेंगे और पढ़ेंगे।", "ये कैप्शन लाइव बन रहे हैं।"},
	"te": {"అందరికీ నమస్కారం, కార్యక్రమానికి స్వాగతం.", "ఈ రోజు మన భాషలోనే వింటాం, చదువుతాం.", "ఈ క్యాప్షన్‌లు ప్రత్యక్షంగా తయారవుతున్నాయి."},
	"kn": {"ಎಲ್ಲರಿಗೂ ನಮಸ್ಕಾರ, ಕಾರ್ಯಕ್ರಮಕ್ಕೆ ಸ್ವಾಗತ.", "ಇಂದು ನಮ್ಮ ಭಾಷೆಯಲ್ಲೇ ಕೇಳುತ್ತೇವೆ, ಓದುತ್ತೇವೆ.", "ಈ ಶೀರ್ಷಿಕೆಗಳು ನೇರವಾಗಿ ತಯಾರಾಗುತ್ತಿವೆ."},
	"ta": {"அனைவருக்கும் வணக்கம், நிகழ்ச்சிக்கு வரவேற்கிறோம்.", "இன்று நம் மொழியிலேயே கேட்போம், படிப்போம்.", "இந்த வசனங்கள் நேரலையில் உருவாகின்றன."},
}

// Fake is a scripted Engine for tests and offline demos: for every Every
// seconds of audio (default 3) it emits an interim with the first half of
// the next line, then the full line as a final.
type Fake struct {
	Lines map[string][]string
	Every float64
}

// Run implements Engine.
func (f Fake) Run(ctx context.Context, cfg Config, audio <-chan []byte, events chan<- Event) error {
	lines := f.Lines[cfg.Lang]
	if len(lines) == 0 {
		lines = DefaultLines[cfg.Lang]
	}
	if len(lines) == 0 {
		lines = DefaultLines["en"]
	}
	every := f.Every
	if every <= 0 {
		every = 3
	}
	per := int64(every * BytesPerSecond)
	var total, mark int64
	n, half := 0, false
	for {
		select {
		case <-ctx.Done():
			return nil
		case chunk, ok := <-audio:
			if !ok {
				return nil
			}
			total += int64(len(chunk))
			at := float64(total) / BytesPerSecond
			line := []rune(lines[n%len(lines)])
			if !half && total-mark >= per/2 {
				half = true
				send(ctx, events, Event{Kind: Interim, Text: string(line[:len(line)/2]), At: at})
			}
			if total-mark >= per {
				mark += per
				half = false
				n++
				send(ctx, events, Event{Kind: Final, Text: string(line), At: at})
			}
		}
	}
}
