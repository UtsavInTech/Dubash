#!/usr/bin/env bash
# Generates synthetic smoke-test recordings with the macOS Indian voices
# (Rishi en_IN, Lekha hi_IN, Geeta te_IN, Soumya kn_IN). They check that
# the benchmark runs end to end against a real provider; they are NOT
# representative of a live speaker (no accent variety, no room noise, no
# hesitation) and must never be quoted as accuracy figures. The audio is
# not committed (see .gitignore): run this script to recreate it.
set -euo pipefail
cd "$(dirname "$0")/recordings"

say_wav() { # voice name text
  say -v "$1" -o "$2.wav" --file-format=WAVE --data-format=LEI16@16000 "$3"
}

say_wav Rishi syn-en-keynote "Good morning everyone. Welcome to the Lenguaraz pilot in Bengaluru. Our partner Sarvam AI will translate this keynote into Hindi, Telugu and Kannada. Registration closes on 15 March, and tickets cost 499 rupees. We will not record this session."
cat > syn-en-keynote.yaml <<'Y'
lang: en
vocabulary: demo-india
tags: [synthetic, names, numbers, negation]
reference: >-
  Good morning everyone. Welcome to the Lenguaraz pilot in Bengaluru. Our
  partner Sarvam AI will translate this keynote into Hindi, Telugu and
  Kannada. Registration closes on 15 March, and tickets cost 499 rupees. We
  will not record this session.
notes: macOS voice Rishi (en_IN). Synthetic; smoke test only.
Y

say_wav Lekha syn-hi-announcement "नमस्ते दोस्तों। आज का कार्यक्रम सुबह 10 बजे शुरू होगा। हमारी मुख्य वक्ता डॉक्टर अनन्या राव हैं। इस सत्र की रिकॉर्डिंग नहीं होगी। कृपया अपने फ़ोन से QR कोड स्कैन करें।"
cat > syn-hi-announcement.yaml <<'Y'
lang: hi
vocabulary: demo-india
tags: [synthetic, names, numbers, negation]
reference: >-
  नमस्ते दोस्तों। आज का कार्यक्रम सुबह 10 बजे शुरू होगा। हमारी मुख्य वक्ता
  डॉक्टर अनन्या राव हैं। इस सत्र की रिकॉर्डिंग नहीं होगी। कृपया अपने फ़ोन से
  QR कोड स्कैन करें।
notes: macOS voice Lekha (hi_IN). Synthetic; smoke test only.
Y

say_wav Lekha syn-hi-codeswitch "आज हम Kubernetes और cloud migration के बारे में बात करेंगे। हमारी team ने पिछले साल 40 percent cost बचाई। यह feature अभी free नहीं है।"
cat > syn-hi-codeswitch.yaml <<'Y'
lang: hi
vocabulary: demo-india
tags: [synthetic, code-switch, technical, numbers, negation]
reference: >-
  आज हम Kubernetes और cloud migration के बारे में बात करेंगे। हमारी team ने
  पिछले साल 40 percent cost बचाई। यह feature अभी free नहीं है।
notes: >-
  macOS voice Lekha (hi_IN) reading Hinglish. The reference keeps English
  words in Latin script, as the codemix style would; a recognizer that
  writes them in Devanagari scores worse on WER here even when it heard
  them right, which is itself worth knowing.
Y

say_wav Geeta syn-te-welcome "అందరికీ నమస్కారం. ఈ కార్యక్రమం హైదరాబాద్‌లో జరుగుతోంది. మా ముఖ్య వక్త డాక్టర్ అనన్య రావు."
cat > syn-te-welcome.yaml <<'Y'
lang: te
vocabulary: demo-india
tags: [synthetic, names]
reference: >-
  అందరికీ నమస్కారం. ఈ కార్యక్రమం హైదరాబాద్‌లో జరుగుతోంది. మా ముఖ్య వక్త
  డాక్టర్ అనన్య రావు.
notes: macOS voice Geeta (te_IN). Synthetic; smoke test only.
Y

say_wav Soumya syn-kn-welcome "ಎಲ್ಲರಿಗೂ ನಮಸ್ಕಾರ. ಈ ಕಾರ್ಯಕ್ರಮ ಬೆಂಗಳೂರಿನಲ್ಲಿ ನಡೆಯುತ್ತಿದೆ. ಪ್ರವೇಶ ಉಚಿತವಲ್ಲ."
cat > syn-kn-welcome.yaml <<'Y'
lang: kn
vocabulary: demo-india
tags: [synthetic, negation]
reference: >-
  ಎಲ್ಲರಿಗೂ ನಮಸ್ಕಾರ. ಈ ಕಾರ್ಯಕ್ರಮ ಬೆಂಗಳೂರಿನಲ್ಲಿ ನಡೆಯುತ್ತಿದೆ. ಪ್ರವೇಶ ಉಚಿತವಲ್ಲ.
notes: >-
  macOS voice Soumya (kn_IN). "ಉಚಿತವಲ್ಲ" (not free) negates with a suffix,
  which the negation word list cannot see: a known limit of that metric.
Y
echo "wrote $(ls *.wav | wc -l | tr -d ' ') recordings"
