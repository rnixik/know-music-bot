(function() {

	var lastTimeout = 0;
	var lastArtistIndex = 49;
	var rows = [];
	var maxArtists = 100;
	var maxTitles = 5;

	var mainIntervalId = setInterval(function() {
	    lastArtistIndex++;
	    $artists = $('.page-genre__artists .artist__name a');
	    console.info("Opening artist", lastArtistIndex, $artists.length);

	    if (lastArtistIndex + 1 > $artists.length || lastArtistIndex + 1 > maxArtists) {
	        console.log("Return", lastArtistIndex, $artists.length);
			localStorage.setItem('sings', JSON.stringify(rows));
	        clearInterval(mainIntervalId);
	        dumpSql();
		    return;
	    }
	    var $artist = $($artists[lastArtistIndex]);
	    if (!$artist || $artist.length == 0) {
	        console.warn("Empty artist at " + lastArtistIndex);
	        return;
	    }

	    console.log("Clicking on", $artist.attr('title'));
	    $artist.click();

        lastTimeout = 0;

	    for (let i = 0; i < maxTitles; i++) {
		    callWithTimeout(function() {
				openTrack(i);
		    }, 2500 + Math.round(Math.random() * 1000));
		    callWithTimeout(function() {
				getLyrics(); 
		    }, 1500);
	    }

	    callWithTimeout(function() {
	        localStorage.setItem('sings', JSON.stringify(rows));
		    window.history.back();
	    }, 200);

	}, 35000 + Math.round(Math.random() * 5000));

	function callWithTimeout(callback, timeout) {
		lastTimeout += timeout;
		setTimeout(function() {
		    callback();
		}, lastTimeout);
	}

	function openTrack(num) {
	    console.info("Opening track " + num);
	    $tracks = $('.page-artist__tracks_top .track');
	    if ($tracks.length == 0) {
	        console.warn("Tracks not found");
	        return;
	    }
	    if (num + 1 > $tracks.length) {
	        console.warn("We have only" + $tracks.length + " tracks");
	        return;
	    }
	    $tracks[num].click();
	}
	
	var prevTitle = '';

	function getLyrics() {
	    var genreLink = $('.page-artist__info .page-artist__summary a').last().attr('href');
	    var $sideBar = $(".sidebar-track").last();

		var title = $sideBar.find('.sidebar-track__title a').last().text();
		var artist = $sideBar.find('.album-summary__pregroup a').last().text();
		var lyrics = $sideBar.find('.sidebar-track__lyric-preview').last().text();

		if (title == '' || artist == '' || lyrics == '') {
		    console.warn("Empty data", title, artist, lyrics);
		    return;
		}

		title = title.replace(/'/g, "\\'");
		artist = artist.replace(/'/g, "\\'");

		lyrics = lyrics.replace(/(?:\r\n|\r|\n)/g, '\\n');
		lyrics = lyrics.replace(/'/g, "\\'");

		var lang = 'en';

		if (/[Р-пр-џ]/.test(lyrics)) {
			lang = 'ru';
		}
		
		var genre = genreLink.replace(/\/genre\//, '');
		
		if (title === prevTitle) {
		    console.warn("Title has not been changed", prevTitle);
		    return;
		}

		var sql = "INSERT INTO `songs` (`artist`, `title`, `lyrics`, `lang`, `genre`) VALUES ('" + artist + "', '" + title + "', '" + lyrics + "', '" + lang + "', '" + genre + "');";
		console.log(sql);
		rows.push(sql);
		prevTitle = title;
		console.log("Total count", rows.length);
	}

})();

var dumpSql = function() {
	var data = localStorage.getItem('sings');
	var rows = JSON.parse(data);
	var string = "";
	for (row in rows) {
	    string += rows[row] + "\n";
	}
	console.log(string);
}
dumpSql();
