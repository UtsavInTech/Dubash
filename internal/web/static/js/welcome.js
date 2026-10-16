const samples = { en: 'Today, we are all here together.', hi: 'आज हम सब एक साथ हैं।', te: 'ఈ రోజు మనమందరం ఇక్కడ కలిసి ఉన్నాము.', kn: 'ಇಂದು ನಾವೆಲ್ಲರೂ ಇಲ್ಲಿ ಒಟ್ಟಿಗೆ ಇದ್ದೇವೆ.', ta: 'இன்று நாம் அனைவரும் இங்கே ஒன்றாக இருக்கிறோம்.' };
const caption = document.getElementById('demo-caption');
for (const button of document.querySelectorAll('#demo-languages button')) {
  button.onclick = () => {
    for (const other of document.querySelectorAll('#demo-languages button')) other.setAttribute('aria-pressed', String(other === button));
    caption.lang = button.dataset.lang; caption.textContent = samples[caption.lang];
  };
}
