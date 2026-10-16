// The public website: the caption preview, and sections that fade in.
const samples = { en: 'Today, we are all here together.', hi: 'आज हम सब एक साथ हैं।', te: 'ఈ రోజు మనమందరం ఇక్కడ కలిసి ఉన్నాము.', kn: 'ಇಂದು ನಾವೆಲ್ಲರೂ ಇಲ್ಲಿ ಒಟ್ಟಿಗೆ ಇದ್ದೇವೆ.', ta: 'இன்று நாம் அனைவரும் இங்கே ஒன்றாக இருக்கிறோம்.' };
const welcome = { en: 'Welcome, everyone.', hi: 'आप सभी का स्वागत है।', te: 'అందరికీ స్వాగతం.', kn: 'ಎಲ್ಲರಿಗೂ ಸ್ವಾಗತ.', ta: 'அனைவருக்கும் வணக்கம்.' };
const caption = document.getElementById('demo-caption');
const past = document.querySelector('.phone-lines .past');
const buttons = document.querySelectorAll('#demo-languages button');
for (const button of buttons) {
  button.onclick = () => {
    for (const other of buttons) other.setAttribute('aria-pressed', String(other === button));
    const lang = button.dataset.lang;
    caption.lang = lang;
    caption.textContent = samples[lang];
    if (past) {
      past.lang = lang;
      past.textContent = welcome[lang];
    }
  };
}

// Fade sections in as they scroll into view; everything is visible without
// JavaScript or with reduced motion.
if ('IntersectionObserver' in window && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
  const seen = new IntersectionObserver((entries) => {
    for (const e of entries) if (e.isIntersecting) { e.target.classList.add('in'); seen.unobserve(e.target); }
  }, { threshold: 0.12 });
  for (const el of document.querySelectorAll('.demo, .card, .band-copy, .india-art, .india-copy, .faq, .cta')) {
    el.classList.add('reveal');
    seen.observe(el);
  }
}

// The menu bar is transparent at the top and gains its background and
// bottom border once the page scrolls.
const nav = document.querySelector('.site-nav');
const onScroll = () => nav?.classList.toggle('scrolled', window.scrollY > 8);
addEventListener('scroll', onScroll, { passive: true });
onScroll();
