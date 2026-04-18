(function () {
  var LANG_NAMES = {
    'zh-CN': '简体中文',
    'zh-TW': '繁體中文',
    'en': 'English',
    'ja': '日本語',
    'ko': '한국어'
  };
  var validLangs = Object.keys(LANG_NAMES);

  // 内联 getCookie（此脚本直接注入 HTML <script> 标签，无法 import 任何模块）
  // 重复实现是有意为之，详见 shared/js/utils/cookie.ts 中的完整版本
function getCookie(name) {
    var match = document.cookie.match('(?:^|;)\\s*' + name + '=([^;]*)');
    return match ? match[1] : null;
  }

  var lang = getCookie('selectedLanguage');
  if (!lang || !LANG_NAMES[lang]) {
    var bl = navigator.language || '';
    if (bl.startsWith('zh')) {
      lang = (bl.includes('TW') || bl.includes('HK')) ? 'zh-TW' : 'zh-CN';
    } else if (bl.startsWith('ja')) {
      lang = 'ja';
    } else if (bl.startsWith('ko')) {
      lang = 'ko';
    } else if (bl.startsWith('en')) {
      lang = 'en';
    } else {
      lang = 'en';
    }
  }

  document.documentElement.lang = lang;
  if (lang !== 'zh-CN') {
    document.documentElement.style.visibility = 'hidden';
  }

  window.__INIT_LANG__ = lang;
})();
