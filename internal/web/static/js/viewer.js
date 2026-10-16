import { NAMES } from './langs.js';

const MAX_LINES = { mobile: 500, screen: 3, overlay: 2 };

// The page speaks the language the reader chose for the captions; keys
// missing in a language fall back to English. The Hindi, Telugu, Kannada
// and Tamil strings were written by the Dubash contributors and still need
// a review by native speakers (docs/DECISIONS.md).
// Modified by the Dubash contributors: Indian languages, translations sent
// per language, pending lines, paused and ended states, resume.
const UI = {
  es: {
    back: '‹ Salas', language: 'Idioma', settings: 'Ajustes de lectura', 'settings-short': 'Ajustes', captions: 'Subtítulos',
    'waiting-title': 'Todavía no hay subtítulos', waiting: 'Van a aparecer acá apenas empiece la charla.',
    scan: 'Escaneá para leer en tu idioma', follow: '↓ Ir a lo último', close: 'Cerrar', size: 'Tamaño del texto',
    'size-0': 'Normal', 'size-1': 'Grande', 'size-2': 'Muy grande', 'size-3': 'Enorme',
    colors: 'Colores', 'theme-auto': 'Como el dispositivo', 'theme-light': 'Fondo claro', 'theme-dark': 'Fondo oscuro',
    'theme-contrast': 'Alto contraste', 'theme-contrast-hint': 'Blanco y amarillo sobre negro',
    text: 'Texto', spacing: 'Más espacio entre letras y líneas', 'spacing-hint': 'Puede ayudar con dislexia o baja visión',
    'show-original': 'Mostrar el original debajo de la traducción', untranslated: '(sin traducir)',
    live: 'En vivo', offline: 'Sin transmisión', download: 'Descargar la transcripción', 'this-talk': 'Esta charla', 'whole-room': 'Toda la sala', notfound: 'No encontramos esa sala.', listen: 'Escuchar',
  },
  en: {
    'pin-title': 'This event asks for a PIN', 'pin-hint': 'The organisers show it in the hall. Scanning the QR code there works too.', pin: 'PIN', enter: 'Open the captions', 'pin-wrong': 'That PIN is not right.', 'pin-wait': 'Too many tries: wait a few minutes.',
    back: '‹ Events', language: 'Language', settings: 'Reading settings', 'settings-short': 'Settings', captions: 'Captions',
    'waiting-title': 'No captions yet', waiting: 'They will show up here as soon as the speaker starts.',
    scan: 'Scan to read in your language', follow: '↓ Jump to latest', close: 'Close', size: 'Text size',
    'size-0': 'Normal', 'size-1': 'Large', 'size-2': 'Very large', 'size-3': 'Huge',
    colors: 'Colours', 'theme-auto': 'Same as my device', 'theme-light': 'Light background', 'theme-dark': 'Dark background',
    'theme-contrast': 'High contrast', 'theme-contrast-hint': 'White and yellow on black',
    text: 'Text', spacing: 'More space between letters and lines', 'spacing-hint': 'Can help with dyslexia or low vision',
    'show-original': 'Show the original under the translation', untranslated: '(not translated)',
    live: 'Live', offline: 'Not streaming', download: 'Download the transcript', 'this-talk': 'This session', 'whole-room': 'The whole event', notfound: 'We could not find that event.', listen: 'Listen',
    paused: 'Paused', 'paused-msg': 'The captions are paused. They will continue here.',
    'ended-title': 'The event has ended', ended: 'Thank you for joining. You can download the transcript below.',
    welcome: 'Welcome to {title}', speaker: 'Speaker: {name}', translating: 'translating…', reconnecting: 'Reconnecting…',
  },
  hi: {
    'pin-title': 'इस कार्यक्रम के लिए PIN चाहिए', 'pin-hint': 'आयोजक इसे हॉल में दिखाते हैं। वहाँ का QR कोड स्कैन करने से भी खुल जाएगा।', pin: 'PIN', enter: 'कैप्शन खोलें', 'pin-wrong': 'यह PIN सही नहीं है।', 'pin-wait': 'बहुत बार कोशिश हुई: कुछ मिनट रुकें।',
    back: '‹ कार्यक्रम', language: 'भाषा', settings: 'पढ़ने की सेटिंग', 'settings-short': 'सेटिंग', captions: 'कैप्शन',
    'waiting-title': 'अभी कोई कैप्शन नहीं', waiting: 'वक्ता के बोलना शुरू करते ही यहाँ दिखेंगे।',
    scan: 'अपनी भाषा में पढ़ने के लिए स्कैन करें', follow: '↓ सबसे नए पर जाएँ', close: 'बंद करें', size: 'अक्षरों का आकार',
    'size-0': 'सामान्य', 'size-1': 'बड़ा', 'size-2': 'बहुत बड़ा', 'size-3': 'सबसे बड़ा',
    colors: 'रंग', 'theme-auto': 'डिवाइस जैसा', 'theme-light': 'हल्की पृष्ठभूमि', 'theme-dark': 'गहरी पृष्ठभूमि',
    'theme-contrast': 'उच्च कंट्रास्ट', 'theme-contrast-hint': 'काले पर सफ़ेद और पीला',
    text: 'अक्षर', spacing: 'अक्षरों और पंक्तियों के बीच ज़्यादा जगह', 'spacing-hint': 'डिस्लेक्सिया या कमज़ोर नज़र में मदद कर सकता है',
    'show-original': 'अनुवाद के नीचे मूल पाठ दिखाएँ', untranslated: '(अनुवाद नहीं हुआ)',
    live: 'लाइव', offline: 'प्रसारण नहीं', download: 'पूरा पाठ डाउनलोड करें', 'this-talk': 'यह सत्र', 'whole-room': 'पूरा कार्यक्रम', notfound: 'यह कार्यक्रम नहीं मिला।', listen: 'सुनें',
    paused: 'रुका हुआ', 'paused-msg': 'कैप्शन अभी रुके हुए हैं। यहीं फिर से शुरू होंगे।',
    'ended-title': 'कार्यक्रम समाप्त हुआ', ended: 'जुड़ने के लिए धन्यवाद। नीचे से पूरा पाठ डाउनलोड कर सकते हैं।',
    welcome: '{title} में आपका स्वागत है', speaker: 'वक्ता: {name}', translating: 'अनुवाद हो रहा है…', reconnecting: 'फिर से जुड़ रहे हैं…',
  },
  te: {
    'pin-title': 'ఈ కార్యక్రమానికి PIN అవసరం', 'pin-hint': 'నిర్వాహకులు దీన్ని హాల్‌లో చూపిస్తారు. అక్కడి QR కోడ్ స్కాన్ చేసినా తెరుచుకుంటుంది.', pin: 'PIN', enter: 'క్యాప్షన్లు తెరవండి', 'pin-wrong': 'ఈ PIN సరైనది కాదు.', 'pin-wait': 'చాలా ప్రయత్నాలు: కొన్ని నిమిషాలు ఆగండి.',
    back: '‹ కార్యక్రమాలు', language: 'భాష', settings: 'చదివే సెట్టింగ్‌లు', 'settings-short': 'సెట్టింగ్‌లు', captions: 'క్యాప్షన్‌లు',
    'waiting-title': 'ఇంకా క్యాప్షన్‌లు లేవు', waiting: 'వక్త మాట్లాడటం మొదలుపెట్టగానే ఇక్కడ కనిపిస్తాయి.',
    scan: 'మీ భాషలో చదవడానికి స్కాన్ చేయండి', follow: '↓ తాజాదానికి వెళ్ళండి', close: 'మూసివేయండి', size: 'అక్షరాల పరిమాణం',
    'size-0': 'సాధారణం', 'size-1': 'పెద్దది', 'size-2': 'చాలా పెద్దది', 'size-3': 'అతి పెద్దది',
    colors: 'రంగులు', 'theme-auto': 'పరికరం లాగానే', 'theme-light': 'లేత నేపథ్యం', 'theme-dark': 'ముదురు నేపథ్యం',
    'theme-contrast': 'అధిక కాంట్రాస్ట్', 'theme-contrast-hint': 'నలుపుపై తెలుపు మరియు పసుపు',
    text: 'అక్షరాలు', spacing: 'అక్షరాలు, పంక్తుల మధ్య ఎక్కువ ఖాళీ', 'spacing-hint': 'డిస్లెక్సియా లేదా తక్కువ చూపు ఉన్నవారికి సహాయపడవచ్చు',
    'show-original': 'అనువాదం కింద అసలు పాఠం చూపించు', untranslated: '(అనువదించలేదు)',
    live: 'ప్రత్యక్షం', offline: 'ప్రసారం లేదు', download: 'పూర్తి పాఠం డౌన్‌లోడ్ చేయండి', 'this-talk': 'ఈ సెషన్', 'whole-room': 'మొత్తం కార్యక్రమం', notfound: 'ఈ కార్యక్రమం కనబడలేదు.', listen: 'వినండి',
    paused: 'ఆపివేయబడింది', 'paused-msg': 'క్యాప్షన్‌లు ప్రస్తుతం ఆగాయి. ఇక్కడే మళ్ళీ కొనసాగుతాయి.',
    'ended-title': 'కార్యక్రమం ముగిసింది', ended: 'పాల్గొన్నందుకు ధన్యవాదాలు. కింద పూర్తి పాఠం డౌన్‌లోడ్ చేసుకోవచ్చు.',
    welcome: '{title}కు స్వాగతం', speaker: 'వక్త: {name}', translating: 'అనువదిస్తోంది…', reconnecting: 'మళ్ళీ కనెక్ట్ అవుతోంది…',
  },
  kn: {
    'pin-title': 'ಈ ಕಾರ್ಯಕ್ರಮಕ್ಕೆ PIN ಬೇಕು', 'pin-hint': 'ಆಯೋಜಕರು ಇದನ್ನು ಸಭಾಂಗಣದಲ್ಲಿ ತೋರಿಸುತ್ತಾರೆ. ಅಲ್ಲಿನ QR ಕೋಡ್ ಸ್ಕ್ಯಾನ್ ಮಾಡಿದರೂ ತೆರೆಯುತ್ತದೆ.', pin: 'PIN', enter: 'ಶೀರ್ಷಿಕೆಗಳನ್ನು ತೆರೆಯಿರಿ', 'pin-wrong': 'ಈ PIN ಸರಿಯಿಲ್ಲ.', 'pin-wait': 'ತುಂಬಾ ಪ್ರಯತ್ನಗಳು: ಕೆಲವು ನಿಮಿಷ ಕಾಯಿರಿ.',
    back: '‹ ಕಾರ್ಯಕ್ರಮಗಳು', language: 'ಭಾಷೆ', settings: 'ಓದುವ ಸೆಟ್ಟಿಂಗ್‌ಗಳು', 'settings-short': 'ಸೆಟ್ಟಿಂಗ್‌ಗಳು', captions: 'ಶೀರ್ಷಿಕೆಗಳು',
    'waiting-title': 'ಇನ್ನೂ ಶೀರ್ಷಿಕೆಗಳಿಲ್ಲ', waiting: 'ಭಾಷಣಕಾರರು ಮಾತನಾಡಲು ಆರಂಭಿಸಿದ ತಕ್ಷಣ ಇಲ್ಲಿ ಕಾಣಿಸುತ್ತವೆ.',
    scan: 'ನಿಮ್ಮ ಭಾಷೆಯಲ್ಲಿ ಓದಲು ಸ್ಕ್ಯಾನ್ ಮಾಡಿ', follow: '↓ ಇತ್ತೀಚಿನದಕ್ಕೆ ಹೋಗಿ', close: 'ಮುಚ್ಚಿ', size: 'ಅಕ್ಷರದ ಗಾತ್ರ',
    'size-0': 'ಸಾಮಾನ್ಯ', 'size-1': 'ದೊಡ್ಡದು', 'size-2': 'ತುಂಬಾ ದೊಡ್ಡದು', 'size-3': 'ಅತಿ ದೊಡ್ಡದು',
    colors: 'ಬಣ್ಣಗಳು', 'theme-auto': 'ಸಾಧನದಂತೆ', 'theme-light': 'ತಿಳಿ ಹಿನ್ನೆಲೆ', 'theme-dark': 'ಗಾಢ ಹಿನ್ನೆಲೆ',
    'theme-contrast': 'ಹೆಚ್ಚಿನ ಕಾಂಟ್ರಾಸ್ಟ್', 'theme-contrast-hint': 'ಕಪ್ಪಿನ ಮೇಲೆ ಬಿಳಿ ಮತ್ತು ಹಳದಿ',
    text: 'ಪಠ್ಯ', spacing: 'ಅಕ್ಷರಗಳು ಮತ್ತು ಸಾಲುಗಳ ನಡುವೆ ಹೆಚ್ಚು ಅಂತರ', 'spacing-hint': 'ಡಿಸ್ಲೆಕ್ಸಿಯಾ ಅಥವಾ ಕಡಿಮೆ ದೃಷ್ಟಿಗೆ ಸಹಾಯವಾಗಬಹುದು',
    'show-original': 'ಅನುವಾದದ ಕೆಳಗೆ ಮೂಲ ಪಠ್ಯ ತೋರಿಸಿ', untranslated: '(ಅನುವಾದವಾಗಿಲ್ಲ)',
    live: 'ನೇರ ಪ್ರಸಾರ', offline: 'ಪ್ರಸಾರವಿಲ್ಲ', download: 'ಪೂರ್ಣ ಪಠ್ಯ ಡೌನ್‌ಲೋಡ್ ಮಾಡಿ', 'this-talk': 'ಈ ಅವಧಿ', 'whole-room': 'ಇಡೀ ಕಾರ್ಯಕ್ರಮ', notfound: 'ಈ ಕಾರ್ಯಕ್ರಮ ಸಿಗಲಿಲ್ಲ.', listen: 'ಕೇಳಿ',
    paused: 'ವಿರಾಮ', 'paused-msg': 'ಶೀರ್ಷಿಕೆಗಳು ಸದ್ಯಕ್ಕೆ ನಿಂತಿವೆ. ಇಲ್ಲೇ ಮುಂದುವರಿಯುತ್ತವೆ.',
    'ended-title': 'ಕಾರ್ಯಕ್ರಮ ಮುಗಿದಿದೆ', ended: 'ಭಾಗವಹಿಸಿದ್ದಕ್ಕೆ ಧನ್ಯವಾದಗಳು. ಕೆಳಗೆ ಪೂರ್ಣ ಪಠ್ಯವನ್ನು ಡೌನ್‌ಲೋಡ್ ಮಾಡಬಹುದು.',
    welcome: '{title}ಗೆ ಸ್ವಾಗತ', speaker: 'ಭಾಷಣಕಾರರು: {name}', translating: 'ಅನುವಾದಿಸಲಾಗುತ್ತಿದೆ…', reconnecting: 'ಮರುಸಂಪರ್ಕಿಸಲಾಗುತ್ತಿದೆ…',
  },
  ta: {
    'pin-title': 'இந்த நிகழ்ச்சிக்கு PIN தேவை', 'pin-hint': 'ஏற்பாட்டாளர்கள் இதை அரங்கில் காட்டுவார்கள். அங்குள்ள QR குறியீட்டை ஸ்கேன் செய்தாலும் திறக்கும்.', pin: 'PIN', enter: 'தலைப்புகளைத் திறக்கவும்', 'pin-wrong': 'இந்த PIN சரியில்லை.', 'pin-wait': 'பல முயற்சிகள்: சில நிமிடங்கள் காத்திருங்கள்.',
    back: '‹ நிகழ்ச்சிகள்', language: 'மொழி', settings: 'வாசிப்பு அமைப்புகள்', 'settings-short': 'அமைப்புகள்', captions: 'வசனங்கள்',
    'waiting-title': 'இன்னும் வசனங்கள் இல்லை', waiting: 'பேச்சாளர் பேசத் தொடங்கியதும் இங்கே தோன்றும்.',
    scan: 'உங்கள் மொழியில் படிக்க ஸ்கேன் செய்யுங்கள்', follow: '↓ சமீபத்தியதற்குச் செல்லவும்', close: 'மூடு', size: 'எழுத்து அளவு',
    'size-0': 'சாதாரணம்', 'size-1': 'பெரியது', 'size-2': 'மிகப் பெரியது', 'size-3': 'மிக மிகப் பெரியது',
    colors: 'நிறங்கள்', 'theme-auto': 'சாதனத்தைப் போல', 'theme-light': 'வெளிர் பின்னணி', 'theme-dark': 'அடர் பின்னணி',
    'theme-contrast': 'உயர் மாறுபாடு', 'theme-contrast-hint': 'கருப்பில் வெள்ளை மற்றும் மஞ்சள்',
    text: 'உரை', spacing: 'எழுத்துகள், வரிகளுக்கு இடையே அதிக இடைவெளி', 'spacing-hint': 'டிஸ்லெக்சியா அல்லது குறைந்த பார்வைக்கு உதவலாம்',
    'show-original': 'மொழிபெயர்ப்பின் கீழ் மூல உரையைக் காட்டு', untranslated: '(மொழிபெயர்க்கப்படவில்லை)',
    live: 'நேரலை', offline: 'ஒளிபரப்பு இல்லை', download: 'முழு உரையைப் பதிவிறக்கவும்', 'this-talk': 'இந்த அமர்வு', 'whole-room': 'முழு நிகழ்ச்சி', notfound: 'இந்த நிகழ்ச்சி கிடைக்கவில்லை.', listen: 'கேளுங்கள்',
    paused: 'இடைநிறுத்தம்', 'paused-msg': 'வசனங்கள் இப்போது நிறுத்தப்பட்டுள்ளன. இங்கேயே மீண்டும் தொடரும்.',
    'ended-title': 'நிகழ்ச்சி முடிந்தது', ended: 'கலந்துகொண்டதற்கு நன்றி. கீழே முழு உரையைப் பதிவிறக்கலாம்.',
    welcome: '{title} நிகழ்ச்சிக்கு வரவேற்கிறோம்', speaker: 'பேச்சாளர்: {name}', translating: 'மொழிபெயர்க்கப்படுகிறது…', reconnecting: 'மீண்டும் இணைக்கிறது…',
  },
  pt: {
    back: '‹ Salas', language: 'Idioma', settings: 'Ajustes de leitura', 'settings-short': 'Ajustes', captions: 'Legendas',
    'waiting-title': 'Ainda não há legendas', waiting: 'Elas vão aparecer aqui assim que a palestra começar.',
    scan: 'Escaneie para ler no seu idioma', follow: '↓ Ir para o mais recente', close: 'Fechar', size: 'Tamanho do texto',
    'size-0': 'Normal', 'size-1': 'Grande', 'size-2': 'Muito grande', 'size-3': 'Enorme',
    colors: 'Cores', 'theme-auto': 'Igual ao dispositivo', 'theme-light': 'Fundo claro', 'theme-dark': 'Fundo escuro',
    'theme-contrast': 'Alto contraste', 'theme-contrast-hint': 'Branco e amarelo sobre preto',
    text: 'Texto', spacing: 'Mais espaço entre letras e linhas', 'spacing-hint': 'Pode ajudar com dislexia ou baixa visão',
    'show-original': 'Mostrar o original abaixo da tradução', untranslated: '(sem tradução)',
    live: 'Ao vivo', offline: 'Sem transmissão', download: 'Baixar a transcrição', 'this-talk': 'Esta palestra', 'whole-room': 'A sala inteira', notfound: 'Não encontramos essa sala.', listen: 'Ouvir',
  },
  fr: {
    back: '‹ Salles', language: 'Langue', settings: 'Réglages de lecture', 'settings-short': 'Réglages', captions: 'Sous-titres',
    'waiting-title': 'Pas encore de sous-titres', waiting: 'Ils apparaîtront ici dès le début de la conférence.',
    scan: 'Scannez pour lire dans votre langue', follow: '↓ Aller au plus récent', close: 'Fermer', size: 'Taille du texte',
    'size-0': 'Normale', 'size-1': 'Grande', 'size-2': 'Très grande', 'size-3': 'Énorme',
    colors: 'Couleurs', 'theme-auto': 'Comme mon appareil', 'theme-light': 'Fond clair', 'theme-dark': 'Fond sombre',
    'theme-contrast': 'Contraste élevé', 'theme-contrast-hint': 'Blanc et jaune sur noir',
    text: 'Texte', spacing: "Plus d'espace entre les lettres et les lignes", 'spacing-hint': 'Peut aider en cas de dyslexie ou de basse vision',
    'show-original': "Afficher l'original sous la traduction", untranslated: '(non traduit)',
    live: 'En direct', offline: 'Pas de diffusion', download: 'Télécharger la transcription', 'this-talk': 'Cette conférence', 'whole-room': 'Toute la salle',
    notfound: 'Salle introuvable.', listen: 'Écouter',
  },
  de: {
    back: '‹ Säle', language: 'Sprache', settings: 'Leseeinstellungen', 'settings-short': 'Einstellungen', captions: 'Untertitel',
    'waiting-title': 'Noch keine Untertitel', waiting: 'Sie erscheinen hier, sobald der Vortrag beginnt.',
    scan: 'Scannen, um in Ihrer Sprache zu lesen', follow: '↓ Zum Neuesten', close: 'Schließen', size: 'Textgröße',
    'size-0': 'Normal', 'size-1': 'Groß', 'size-2': 'Sehr groß', 'size-3': 'Riesig',
    colors: 'Farben', 'theme-auto': 'Wie mein Gerät', 'theme-light': 'Heller Hintergrund', 'theme-dark': 'Dunkler Hintergrund',
    'theme-contrast': 'Hoher Kontrast', 'theme-contrast-hint': 'Weiß und Gelb auf Schwarz',
    text: 'Text', spacing: 'Mehr Abstand zwischen Buchstaben und Zeilen', 'spacing-hint': 'Kann bei Legasthenie oder Sehschwäche helfen',
    'show-original': 'Original unter der Übersetzung anzeigen', untranslated: '(nicht übersetzt)',
    live: 'Live', offline: 'Keine Übertragung', download: 'Transkript herunterladen', 'this-talk': 'Dieser Vortrag', 'whole-room': 'Der ganze Saal',
    notfound: 'Diesen Saal gibt es nicht.', listen: 'Anhören',
  },
  it: {
    back: '‹ Sale', language: 'Lingua', settings: 'Impostazioni di lettura', 'settings-short': 'Impostazioni', captions: 'Sottotitoli',
    'waiting-title': 'Ancora nessun sottotitolo', waiting: 'Appariranno qui non appena inizia il talk.',
    scan: 'Scansiona per leggere nella tua lingua', follow: '↓ Vai al più recente', close: 'Chiudi', size: 'Dimensione del testo',
    'size-0': 'Normale', 'size-1': 'Grande', 'size-2': 'Molto grande', 'size-3': 'Enorme',
    colors: 'Colori', 'theme-auto': 'Come il dispositivo', 'theme-light': 'Sfondo chiaro', 'theme-dark': 'Sfondo scuro',
    'theme-contrast': 'Alto contrasto', 'theme-contrast-hint': 'Bianco e giallo su nero',
    text: 'Testo', spacing: 'Più spazio tra lettere e righe', 'spacing-hint': 'Può aiutare con dislessia o ipovisione',
    'show-original': "Mostra l'originale sotto la traduzione", untranslated: '(non tradotto)',
    live: 'In diretta', offline: 'Nessuna trasmissione', download: 'Scarica la trascrizione', 'this-talk': 'Questo talk', 'whole-room': 'Tutta la sala',
    notfound: 'Sala non trovata.', listen: 'Ascolta',
  },
};

const params = new URLSearchParams(location.search);
const roomId = decodeURIComponent(location.pathname.split('/').pop());
const mode = Object.hasOwn(MAX_LINES, params.get('mode')) ? params.get('mode') : 'mobile';
document.body.dataset.mode = mode;
if (mode === 'overlay') document.documentElement.classList.add('transparent');
if (mode === 'screen') document.documentElement.dataset.theme = params.get('theme') ?? 'dark';
// OBS and projector setups are configured by URL, not by the settings dialog.
if (/^[0-3]$/.test(params.get('size') ?? '')) document.documentElement.dataset.size = params.get('size');
if (mode !== 'mobile' && /^[1-5]$/.test(params.get('lines') ?? '')) MAX_LINES[mode] = Number(params.get('lines'));
// Seconds a line stays up with nothing new (overlay default 8; 0 = forever),
// so a pause or the end of a talk does not leave old text burned in the stream.
const HOLD = Number(params.get('hold') ?? (mode === 'overlay' ? 8 : 0)) || 0;

const $ = (id) => document.getElementById(id);
const segments = new Map(); // id → segment
let room;
let lang;
let interim = '';
let live = false;
let state = { paused: false, ended: false };
let talk = { n: 0, title: '' }; // the room's current talk, from its status
let canExport = true; // the event's exports policy lets this reader download
let events; // the EventSource, reopened when the reader changes language
// A line whose translation has not come after this long is shown as not
// translated instead of "translating…".
const PENDING_MS = 20000;

// ---------- preferences ----------
const PREFS_KEY = 'dubash.prefs';
const prefs = { theme: 'auto', size: 0, spacing: false, orig: false };
try { Object.assign(prefs, JSON.parse(localStorage.getItem(PREFS_KEY) || '{}')); } catch { /* private mode */ }

function applyPrefs() {
  const d = document.documentElement;
  if (mode === 'mobile') {
    if (prefs.theme === 'auto') delete d.dataset.theme;
    else d.dataset.theme = prefs.theme;
  }
  if (prefs.spacing) d.dataset.spacing = 'wide';
  else delete d.dataset.spacing;
  if (prefs.size > 0) d.dataset.size = String(prefs.size);
  else delete d.dataset.size;
  for (const b of $('sizes').children) b.setAttribute('aria-pressed', String(Number(b.dataset.size) === prefs.size));
  for (const input of $('prefs').querySelectorAll('input[type=radio]')) input.checked = prefs[input.name] === input.value;
  $('pref-orig').checked = prefs.orig;
  $('pref-spacing').checked = prefs.spacing;
}

function savePrefs() {
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(prefs)); } catch { /* private mode */ }
  applyPrefs();
  renderLines();
}

function wirePrefs() {
  $('prefs-open').onclick = () => $('prefs').showModal();
  for (const b of $('sizes').children) b.onclick = () => { prefs.size = Number(b.dataset.size); savePrefs(); };
  $('prefs').addEventListener('change', (e) => {
    const t = e.target;
    if (t.type === 'radio') prefs[t.name] = t.value;
    if (t.id === 'pref-orig') prefs.orig = t.checked;
    if (t.id === 'pref-spacing') prefs.spacing = t.checked;
    savePrefs();
  });
  // A click on the backdrop closes the sheet.
  $('prefs').addEventListener('click', (e) => { if (e.target === $('prefs')) $('prefs').close(); });
  applyPrefs();
}

// ---------- language of the page ----------
function t(key, vars = {}) {
  const s = (UI[lang] ?? UI.en)[key] ?? UI.en[key] ?? key;
  return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? '');
}

function translatePage() {
  document.documentElement.lang = lang;
  $('captions').lang = lang;
  for (const node of document.querySelectorAll('[data-i18n]')) node.textContent = t(node.dataset.i18n);
  for (const node of document.querySelectorAll('[data-i18n-label]')) {
    node.setAttribute('aria-label', t(node.dataset.i18nLabel));
  }
  $('orig-row').hidden = lang === room.source;
  setLive(live);
}

function pickLang(langs) {
  if (langs.includes(params.get('lang'))) return params.get('lang');
  if (mode === 'mobile') {
    // A QR code or a shared link without ?lang: follow the reader's browser.
    for (const l of navigator.languages ?? []) {
      const code = l.slice(0, 2).toLowerCase();
      if (langs.includes(code)) return code;
    }
  }
  return room.targets[0] ?? room.source;
}

// ---------- captions ----------
// pending reports a line whose translation into lang is still on its way:
// it arrived live, after the replay, and not too long ago.
function pending(seg) {
  return seg.lang !== lang && !seg.translations?.[lang] && !seg.failed?.[lang] &&
    seg.live && Date.now() - seg.live < PENDING_MS;
}

function line(seg) {
  const p = document.createElement('p');
  const span = document.createElement('span');
  const translated = seg.lang === lang ? seg.text : seg.translations?.[lang];
  span.textContent = translated ?? seg.text;
  if (!translated) span.lang = seg.lang;
  p.append(span);
  if (!translated && seg.lang !== lang) {
    const waiting = pending(seg);
    p.classList.toggle('pending', waiting);
    const note = document.createElement('small');
    note.className = 'note';
    note.textContent = waiting ? t('translating') : t('untranslated');
    p.append(note);
  }
  if (translated && seg.lang !== lang && prefs.orig && mode === 'mobile') {
    const orig = document.createElement('small');
    orig.className = 'orig';
    orig.lang = seg.lang;
    orig.textContent = seg.text;
    p.append(orig);
  }
  return p;
}

function showsInterim() {
  return lang === room.source && interim !== '';
}

function nearBottom() {
  return window.innerHeight + window.scrollY >= document.body.scrollHeight - 160;
}

function renderLines({ fresh = false } = {}) {
  if (!room) return;
  const stick = nearBottom();
  const keep = Math.max(1, MAX_LINES[mode] - (mode !== 'mobile' && showsInterim() ? 1 : 0));
  // The projector and the overlay show only finished lines in the chosen
  // language: a line waiting for its translation would flicker.
  const ordered = [...segments.values()].filter((s) => mode === 'mobile' || !pending(s))
    .sort((a, b) => a.id - b.id).slice(-keep);
  $('lines').replaceChildren(...ordered.map(line));
  renderInterim();
  if (mode !== 'mobile') return;
  if (stick) window.scrollTo(0, document.body.scrollHeight);
  else if (fresh) $('follow').hidden = false;
}

function renderInterim() {
  const span = document.createElement('span');
  span.textContent = interim;
  $('interim').replaceChildren(...(showsInterim() ? [span] : []));
  $('empty').hidden = mode === 'overlay' || segments.size > 0 || showsInterim();
}

function setLive(on) {
  live = on;
  const badge = $('status');
  const paused = on && state.paused;
  badge.textContent = paused ? t('paused') : on ? t('live') : t('offline');
  badge.classList.toggle('live', on && !paused);
  badge.classList.toggle('paused', paused);
  renderBanner();
}

// The banner says why no captions are coming: paused, ended, reconnecting.
let reconnecting = false;
function renderBanner() {
  const b = $('banner');
  let title = '';
  let text = '';
  if (state.ended && !live) [title, text] = [t('ended-title'), t('ended')];
  else if (live && state.paused) [title, text] = [t('paused'), t('paused-msg')];
  else if (reconnecting) text = t('reconnecting');
  b.hidden = !text;
  const strong = document.createElement('strong');
  strong.textContent = title;
  b.replaceChildren(...(title ? [strong] : []), text);
}

function renderWelcome() {
  const w = $('welcome');
  if (!room) return;
  const parts = [t('welcome', { title: room.title })];
  if (room.speaker) parts.push(t('speaker', { name: room.speaker }));
  w.textContent = parts.join(' · ');
  w.hidden = false;
}

function renderLangs() {
  const langs = [room.source, ...room.targets];
  $('langs').replaceChildren(...langs.map((l) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = NAMES[l] ?? l;
    b.lang = l;
    b.setAttribute('aria-pressed', String(l === lang));
    b.onclick = () => {
      if (l === lang) return;
      lang = l;
      hush();
      params.set('lang', l);
      history.replaceState(null, '', `?${params}`);
      renderAll();
      connect(); // the server translates into the languages being read
    };
    return b;
  }));
}

function renderExports() {
  document.querySelector('.downloads').hidden = !canExport;
  if (!canExport) return;
  const names = { vtt: 'VTT (web subtitles)', srt: 'SRT (video subtitles)', txt: 'TXT (plain text)' };
  const group = (label, query) => {
    const links = ['txt', 'srt', 'vtt'].map((f) => {
      const a = document.createElement('a');
      a.href = `/export/${roomId}/${f}?lang=${lang}${query}`;
      a.textContent = f.toUpperCase();
      a.title = names[f];
      a.download = '';
      const li = document.createElement('li');
      li.append(a);
      return li;
    });
    const h = document.createElement('h3');
    h.textContent = label;
    const ul = document.createElement('ul');
    ul.append(...links);
    const div = document.createElement('div');
    div.append(h, ul);
    return div;
  };
  const groups = [group(t('whole-room'), '')];
  if (talk.n > 0) groups.unshift(group(talk.title ? `${t('this-talk')}: ${talk.title}` : t('this-talk'), `&talk=${talk.n}`));
  $('exports').replaceChildren(...groups);
}

function renderAll() {
  translatePage();
  renderWelcome();
  renderLangs();
  renderExports();
  renderLines();
  $('screen-title').textContent = `${room.title} · ${NAMES[lang] ?? lang}`;
}

async function showQR() {
  if (mode !== 'screen' || params.get('qr') === '0') return;
  const img = $('qr').querySelector('img');
  img.src = `/qr/${encodeURIComponent(roomId)}`;
  img.alt = `QR: ${room.title}`;
  $('qr').hidden = false;
  try {
    const { url } = await (await fetch('/api/site')).json();
    const p = document.createElement('p');
    p.textContent = `${url.replace(/^https?:\/\//, '')}/r/${roomId}`;
    $('qr').append(p);
  } catch { /* the QR alone is enough */ }
}

function wireFollow() {
  window.addEventListener('scroll', () => { if (nearBottom()) $('follow').hidden = true; }, { passive: true });
  $('follow').onclick = () => {
    window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' });
    $('follow').hidden = true;
  };
}

async function main() {
  lang = UI[params.get('lang')] ? params.get('lang') : 'en';
  const rooms = await loadRooms();
  room = rooms.find((r) => r.id === roomId);
  if (!room) {
    const p = document.createElement('p');
    p.textContent = t('notfound');
    $('lines').replaceChildren(p);
    return;
  }
  lang = pickLang([room.source, ...room.targets]);
  document.title = `${room.title} · Dubash`;
  $('title').textContent = room.title;
  translatePage();
  // An event with a PIN opens after it (or after scanning the hall's QR
  // code, which sets the same pass).
  const access = await fetch(`/api/rooms/${encodeURIComponent(roomId)}/access`)
    .then((r) => r.json()).catch(() => ({ allowed: true, exports: true }));
  canExport = access.exports;
  if (!access.allowed) {
    await askPIN();
    canExport = (await (await fetch(`/api/rooms/${encodeURIComponent(roomId)}/access`)).json()).exports;
  }
  wirePrefs();
  wireFollow();
  wireListen();
  setLive(room.live);
  renderAll();
  showQR();
  keepOverlayLoadable();

  connect();
  // Pending lines turn into "not translated" when their time is up.
  setInterval(() => { if ([...segments.values()].some((s) => s.live)) renderLines(); }, 5000);
}

// askPIN shows the PIN form and resolves once the server accepts a PIN.
function askPIN() {
  return new Promise((resolve) => {
    const form = document.createElement('form');
    form.className = 'pin-form';
    const h = document.createElement('h2');
    h.textContent = t('pin-title');
    const hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = t('pin-hint');
    const label = document.createElement('label');
    label.className = 'field';
    const span = document.createElement('span');
    span.textContent = t('pin');
    const input = document.createElement('input');
    Object.assign(input, { name: 'pin', inputMode: 'numeric', autocomplete: 'off', required: true, maxLength: 12, pattern: '[0-9]*' });
    label.append(span, input);
    const button = document.createElement('button');
    button.className = 'button';
    button.type = 'submit';
    button.textContent = t('enter');
    const error = document.createElement('p');
    error.className = 'error';
    error.setAttribute('role', 'alert');
    form.append(h, hint, label, button, error);
    form.onsubmit = async (e) => {
      e.preventDefault();
      button.disabled = true;
      try {
        const res = await fetch(`/api/rooms/${encodeURIComponent(roomId)}/unlock`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ pin: input.value.trim() }),
        });
        if (res.ok) {
          $('lines').replaceChildren();
          resolve();
          return;
        }
        error.textContent = t(res.status === 429 ? 'pin-wait' : 'pin-wrong');
      } catch (err) {
        error.textContent = err.message;
      }
      button.disabled = false;
      input.select();
    };
    $('empty').hidden = true;
    $('lines').replaceChildren(form);
    input.focus();
  });
}

// connect opens the caption stream for the reader's language. On a dropped
// connection the browser reconnects by itself and sends the id of the last
// line it got (Last-Event-ID), and the server sends only what was missed.
function connect() {
  events?.close();
  caughtUp = false;
  events = new EventSource(`/events/${encodeURIComponent(roomId)}?lang=${encodeURIComponent(lang)}`);
  const onLine = (e) => {
    const seg = JSON.parse(e.data);
    const known = segments.get(seg.id);
    const fresh = !known;
    // Keep translations already received: a replay sends the line again.
    seg.translations = e.type === 'correction' ? {} : { ...(known?.translations ?? {}), ...(seg.translations ?? {}) };
    seg.failed = {};
    if (caughtUp) seg.live = Date.now();
    segments.set(seg.id, seg);
    if (segments.size > MAX_LINES.mobile + 50) segments.delete(Math.min(...segments.keys()));
    if (seg.lang === room.source) interim = '';
    renderLines({ fresh });
    if (fresh) freshText();
    if (fresh && caughtUp && seg.lang === lang) speak(seg.text);
  };
  events.addEventListener('final', onLine);
  events.addEventListener('correction', onLine);
  events.addEventListener('translation', (e) => {
    const tr = JSON.parse(e.data);
    const seg = segments.get(tr.segment);
    // A translation of an earlier revision of a corrected line is stale.
    if (!seg || tr.lang !== lang || (tr.rev ?? 0) < (seg.rev ?? 0)) return;
    if (tr.error) seg.failed[tr.lang] = true;
    else seg.translations[tr.lang] = tr.text;
    renderLines();
    if (!tr.error) {
      freshText();
      if (caughtUp) speak(tr.text);
    }
  });
  events.addEventListener('interim', (e) => {
    interim = JSON.parse(e.data).text;
    if (lang === room.source) {
      renderInterim();
      if (interim) freshText();
    }
  });
  events.addEventListener('status', (e) => {
    const st = JSON.parse(e.data);
    state = { paused: !!st.paused, ended: !!st.ended };
    // The operator says which language is spoken in each transmission;
    // that one is the original, the room's others are translations.
    if (st.source && st.source !== room.source) {
      room.source = st.source;
      room.targets = st.targets ?? [];
      renderAll();
    }
    if (st.talk !== talk.n || (st.talkTitle ?? '') !== talk.title) {
      talk = { n: st.talk ?? 0, title: st.talkTitle ?? '' };
      renderExports();
    }
    setLive(st.live);
    if (!live) {
      interim = '';
      renderInterim();
    }
  });
  events.addEventListener('caught-up', () => {
    caughtUp = true;
    // The server translates the history into a language its reader just
    // opened: show those lines as on their way, for a while.
    const now = Date.now();
    for (const s of segments.values()) if (s.lang !== lang && !s.translations?.[lang]) s.live = now;
    renderLines();
  });
  events.onopen = () => { reconnecting = false; renderBanner(); };
  events.onerror = () => {
    caughtUp = false;
    reconnecting = events.readyState !== EventSource.CLOSED;
    setLive(false);
  };
}

// ---------- listen: the phone reads the captions aloud ----------
// With headphones this turns the captions into simultaneous interpretation,
// using the voices the reader's device already has: no server cost, and
// nothing leaves the phone. Off on every load, since browsers only let a
// page speak after a tap.
const speech = window.speechSynthesis;
const queue = []; // lines waiting to be read, oldest first
let listening = false;
let current; // the utterance being read, if any
let wakeLock;

function wireListen() {
  if (mode !== 'mobile' || !speech || !window.SpeechSynthesisUtterance) return;
  $('listen').hidden = false;
  $('listen').onclick = () => setListening(!listening);
  document.addEventListener('visibilitychange', () => { if (listening && document.visibilityState === 'visible') keepAwake(); });
}

function setListening(on) {
  listening = on;
  $('listen').setAttribute('aria-pressed', String(on));
  // Screen readers would otherwise announce every line on top of the voice.
  $('lines').setAttribute('aria-live', on ? 'off' : 'polite');
  hush();
  if (on) {
    // iOS only lets a page speak once it has spoken inside a tap: start
    // with the last line, or a silent one before the talk begins.
    const last = [...segments.values()].sort((a, b) => a.id - b.id).at(-1);
    const text = last && (last.lang === lang ? last.text : last.translations?.[lang]);
    if (text) speak(text);
    if (!current) {
      const primer = new SpeechSynthesisUtterance(' ');
      primer.volume = 0;
      speech.speak(primer);
    }
    keepAwake();
  } else {
    wakeLock?.release().catch(() => {});
    wakeLock = undefined;
  }
}

function hush() {
  queue.length = 0;
  current = undefined;
  // Chrome may swallow a speak() that follows cancel() on an idle engine.
  if (speech?.speaking || speech?.pending) speech.cancel();
}

// speak reads text, which is always in the reader's language: never read a
// line in a language the reader did not pick.
function speak(text) {
  if (!listening || !text) return;
  queue.push(text);
  // Stay live: when the talk runs ahead of the voice, drop the oldest lines.
  while (queue.length > 3) queue.shift();
  if (!current) next();
}

function next() {
  current = undefined;
  const text = queue.shift();
  if (!text || !listening) return;
  const u = new SpeechSynthesisUtterance(text);
  u.lang = lang;
  const voice = pickVoice(lang);
  if (voice) u.voice = voice;
  // Catch up by reading faster while lines are waiting.
  u.rate = Math.min(1 + 0.15 * queue.length, 1.45);
  // Ignore the end of an utterance that hush() already cancelled.
  u.onend = u.onerror = () => { if (current === u) next(); };
  current = u;
  speech.speak(u);
}

function pickVoice(code) {
  const voices = speech.getVoices().filter((v) => v.lang.toLowerCase().replace('_', '-').startsWith(code));
  return voices.find((v) => v.localService && v.default) ?? voices.find((v) => v.localService) ?? voices[0];
}

// Phones pause the page when the screen turns off; keep it on while listening.
async function keepAwake() {
  try { wakeLock = await navigator.wakeLock?.request('screen'); } catch { /* not supported or denied */ }
}

// ---------- hold: fade out after silence ----------
let caughtUp = false;
let holdTimer;
function freshText() {
  if (!HOLD || !caughtUp) return;
  $('captions').classList.remove('idle');
  clearTimeout(holdTimer);
  holdTimer = setTimeout(() => $('captions').classList.add('idle'), HOLD * 1000);
}

// OBS often loads its sources before the server is up: keep retrying.
async function loadRooms() {
  for (let wait = 1000; ; wait = Math.min(wait * 2, 10000)) {
    try {
      const res = await fetch('/api/rooms');
      if (res.ok) return await res.json();
    } catch { /* server not up yet */ }
    await new Promise((r) => setTimeout(r, wait));
  }
}

// ...but if the server is down when OBS loads the page itself, the retry
// above never runs. Save this page and what it loaded so /sw.js can serve
// them next time; the saved copy then waits in loadRooms. Needs a secure
// origin (localhost or https); elsewhere the overlay reloads by hand.
async function keepOverlayLoadable() {
  if (mode !== 'overlay' || !('serviceWorker' in navigator)) return;
  try {
    await navigator.serviceWorker.register('/sw.js');
    await document.fonts.ready; // so the saved copy has the typeface too
    const assets = performance.getEntriesByType('resource').map((e) => e.name)
      .filter((u) => new URL(u).origin === location.origin && new URL(u).pathname.startsWith('/static/'));
    await (await caches.open('overlay')).addAll([location.href, ...assets]);
  } catch { /* the overlay works without it */ }
}

if (HOLD) $('captions').classList.add('idle');
main();
