var title = $('.sidebar-track .sidebar-track__title a').last().text();
var artist = $('.sidebar-track .album-summary__pregroup a').last().text();
var lyrics = $('.sidebar-track .sidebar-track__lyric-preview').last().text();
console.log(lyrics);
lyrics = lyrics.replace(/(?:\r\n|\r|\n)/g, '\\n');
var lang = 'ru';
var genre = 'alternative_rock';

var sql = "INSERT INTO `songs` (`artist`, `title`, `lyrics`, `lang`, `genre`) VALUES ('" + artist + "', '" + title + "', '" + lyrics + "', '" + lang + "', '" + genre + "');";
console.log(sql);
