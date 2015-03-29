"use strict"

var global;

function defineGlobals() {
    global = {
        constraints:    qs(document, "span#constraints"),
        xhrDisplay:     qs(document, "div#xhr_msg_display"),
        tdiv:           qs(document, "div.thread"),
        thisHan:        qs(document, "div#this_han"),
        submit:         qs(document, "input#submit_post"),
        comment:        getVisibleElem("textarea.comment"),
        upload:         getVisibleElem("input.upload"),
        pageType:       qs(document, "body").getAttribute("data-page_type"),
        viewMode:       qs(document, "body").getAttribute("data-view_mode"),
        nextColor:      makeColorGenerator()
    };
}

function qsa(elem, query) { return elem.querySelectorAll(query); }
function qs(elem, query) { return elem.querySelector(query); }

function mapQsa(root, query, func) {
    return map(qsa(root, query), func);
}

function map(elems, f) {
    var out = [];
    for (var i = 0; i < elems.length; i++) {
        var e = elems[i];
        out.push(f(e));
    }
    return out
}

function rowByLid(lid) {
    return constrainedRowByLid(document, lid);
}

function constrainedRowByLid(e, lid) {
    return qs(e, 'article.post_row[data-post_lid="' + lid + '"]');
}

function allRowsBut(lid) {
    return qsa(document, 'article.post_row:not([data-post_lid="' + lid + '"])');
}

function qsael(elem, query, eventName, func) {
    mapQsa(elem, query, function(e) {
        e.addEventListener(eventName, func, false);
    });
}

function addRowHandlers(row) {
    qsael(row, "aside.post_header", "click",        highlightUserPosts);
    qsael(row, "aside.post_header", "mouseover",    highlightHeader);
    qsael(row, "aside.post_header", "mouseout",     unhighlightHeader);
    qsael(row, "a.post_no",         "click",        quotePost);
    qsael(row, "a.post_no",         "mouseover",    postNoHover);
    qsael(row, "a.post_no",         "mouseout",     restoreAllRows);
    qsael(row, "a.reply_target",    "click",        highlightTarget);
    qsael(row, "a.reply_target",    "mouseover",    replyTargetHover);
    qsael(row, "a.reply_target",    "mouseout",     restoreAllRows);
    qsael(row, "a.reply_link",      "click",        stickyReplyLink);
    qsael(row, "a.reply_link",      "mouseover",    replyLinkHover);
    qsael(row, "a.reply_link",      "mouseout",     restoreAllRows);
    qsael(row, "a.report_post",     "mouseover",    highlightReport);
    qsael(row, "a.report_post",     "mouseout",     unhighlightNotifier);
    qsael(row, "a.fold_posts",      "click",        handleFold);
    qsael(row, "a.fold_posts",      "mouseover",    highlightFold);
    qsael(row, "a.fold_posts",      "mouseout",     unhighlightNotifier);
    addThumbHandler(row);
}

function addThumbHandler(row) {
    var type = qs(row, "aside.post_header").getAttribute("data-media_type");
    var f = {
        "image": expandWith(insertFullImage),
        "video": expandWith(insertFullVideo),
        "audio": expandWith(insertFullAudio)
    }[type];

    if (f) { qsael(row, "img.thumb", "click", f) }
}

function handleFold(e) {
    if (global.pageType != "thread") { return }
    haltEvent(e);
    toggleFoldConvo(this);
    return false;
}

function toggleFoldConvo(e) {
    var row = e.parentNode;
    var active = row.getAttribute("data-pivot");
    unfoldAll();

    if (!active) {
        foldConvo(row)
    }
}

function foldConvo(row) {
    setFold(row);
    traverseUp(row, setFold);
    traverseDown(row, setFold);
    foldUnmarked();
    setRowFoldControl(row, "◆", "true");
    row.scrollIntoView();
}

function setRowFoldControl(row, marker, val) {
    var e = qs(row, "a.fold_posts");
    row.setAttribute("data-pivot", val);
    e.textContent = marker;
}

function foldUnmarked() {
    mapQsa(document, "article.post_row:not([data-folded='true'])", function(row) {
        row.style.display = "none";
    });
}

function unfoldAll() {
    mapQsa(document, "article.post_row", function(row) {
        row.style.display = null;
        unsetFold(row);
        setRowFoldControl(row, "◇", "");
    });
}

function setFold(row) {
    row.setAttribute("data-folded", "true");
}

function unsetFold(row) {
    row.setAttribute("data-folded", "");
}

function traverseUp(row, f) {
    var targetId = qs(row, "a.reply_target").getAttribute("data-target");

    if (targetId != 0) {
       var upperRow = rowByLid(targetId);
       f(upperRow);
       traverseUp(upperRow, f);
    }
}

function traverseDown(row, f) {
    map(getReplies(row), function(lid) {
        var lowerRow = rowByLid(lid);
        f(lowerRow);
        traverseDown(lowerRow, f);
    });
}

function insertFullImage(row, thumb) {
    var img = insertFullMedia(row, thumb, "img");
    img.addEventListener("click", function() {
        expandWith(insertFullImage).call(thumb);
    }, false);
}

function insertFullVideo(row, thumb) {
    pauseAllMedia();
    var vid = insertFullMedia(row, thumb, "video");
    vid.setAttribute("autoplay", true);
    vid.setAttribute("controls", true);
    vid.setAttribute("loop", true);
}

function insertFullAudio(row, thumb) {
    pauseAllMedia();
    var aud = insertFullMedia(row, thumb, "audio");
    aud.setAttribute("autoplay", true);
    aud.setAttribute("controls", true);
}

function insertFullMedia(row, thumb, type) {
    var body = qs(row, "div.comment_body");
    var text = qs(row, "span.comment_text");
    var path = thumb.getAttribute("data-path");
    var e = document.createElement(type);
    e.setAttribute("src", "/i/" + path);
    e.classList.add("full");
    e.style.maxWidth = "100%";
    e.style.maxHeight = "100%";
    body.insertBefore(e, text);
    return e
}

function pauseAllMedia() {
    mapQsa(document, "audio, video", function(e) {
        e.pause();
    });
}

function expandWith(f) {
    return function(e) { expandFull(e, this, f) }
}

function expandFull(e, elem, f) {
    haltEvent(e);
    var row = elem.parentNode.parentNode.parentNode.parentNode;
    var full = qs(row, '.full');
    full ? full.parentNode.removeChild(full) : f(row, elem);
}

function haltEvent(e) {
    if (e) {
        e.stopPropagation();
        e.preventDefault();
    }
}

function highlightReport() {
    showRowNotifier(this.parentNode, "REPORT", "#dc9190", "black");
}

function highlightFold() {
    showRowNotifier(this.parentNode, "FOLD", "#cbc0dc", "black");
}

function unhighlightNotifier() {
    showRowNotifier(this.parentNode, null, null, null);
}

function highlightTarget() {
    forceRestoreAllRows();
    replyTargetHover.call(this);
    var originRow = rowByLid(this.getAttribute("data-target"));
    stickyRowHighlight(originRow, false, true, false, true);
}

function replyLinkHover() {
    clearAllRows();
    var row = this.parentNode.parentNode.parentNode;
    var replyRow = getReplyAnchorRow(this);
    var postNo = qs(row, "a.post_no");
    setRowHighlight(row, false, true, false, false);
    setRowHighlight(replyRow, false, false, true, postNo);
}

function stickyReplyLink() {
    forceRestoreAllRows();
    replyLinkHover.call(this);
    var replyRow = getReplyAnchorRow(this);
    stickyRowHighlight(replyRow, false, true, false, true);
}

function getReplyAnchorRow(e) {
    var row = e.parentNode.parentNode.parentNode;
    var postNo = qs(row, "a.post_no");
    return rowByLid(e.getAttribute("data-reply"));
}

function highlightHeader() {
    var phc = postHeaderColor(this);
    var row = this.parentNode.parentNode;
    setRowHighlight(row, phc, true, true, false);
}

function unhighlightHeader() {
    var row = this.parentNode.parentNode;
    setRowHighlight(row, false, true, true, false);
}

function highlightUserPosts() {
    var phc = postHeaderColor(this);
    var userId = this.getAttribute("data-user");

    map(allPostsByUser(userId), function(row) {
        if (row.getAttribute("data-active_post_highlight")) {
            stickyRowHighlight(row, false, true, true, false);
            setRowHighlight(row, false, false, false, false);
            row.setAttribute("data-active_post_highlight", "");
        } else {
            setRowHighlight(row, phc, true, true, phc);
            stickyRowHighlight(row, true, false, false, true);
            row.setAttribute("data-active_post_highlight", "true");
        }
    });
}

function postHeaderColor(ph) {
    var hsl = {};
    hsl.hue         = ph.getAttribute("data-hue");
    hsl.saturation  = ph.getAttribute("data-saturation");
    hsl.lightness   = ph.getAttribute("data-lightness");
    return altLightColor(hslToString(hsl));
}

function allPostsByUser(id) {
    return mapQsa(document, 'aside.post_header[data-user="' +id+ '"]', function (header) {
        return header.parentNode.parentNode;
    });
}

function quotePost() {
    var postNo = this;
    var lid = postNo.innerHTML.trim();
    var row = rowByLid(lid);
    forceRestoreAllRows();

    if (postNo.getAttribute("data-hi_bgc")) {
        setRowHighlight(row, false, true, false, postNo);
    } else {
        setRowHighlight(row, false, true, false, "#CCC7B7");
    }

    stickyRowHighlight(row, false, true, false, true);
    getVisibleElem("input.reply_to").value = lid;
    getVisibleElem("textarea.comment").focus();
    getVisibleElem("input.reply_to").scrollIntoView();
}

function decorateRow(row) {
    if (row.getAttribute("data-omission")) { return }
    setDefaultRowColors(row);
    colorPostLinks(row);
    addRowHandlers(row);
}

function postNoHover() {
    var postNo = this;
    if (!postNo.getAttribute("data-hi_bgc")) {
        return;
    }

    clearAllRows();
    var parentRow = postNo.parentNode.parentNode;
    setRowHighlight(parentRow, false, true, false, false);

    map(getReplies(parentRow), function(lid) {
        setRowHighlight(rowByLid(lid), false, false, true, postNo);
    });
}

function replyTargetHover() {
    var replyTarget = this;
    if (!replyTarget.getAttribute("data-hi_bgc")) {
        return;
    }

    clearAllRows();
    var originRow = rowByLid(replyTarget.getAttribute("data-target"));
    setRowHighlight(originRow, false, true, false, replyTarget);

    map(getReplies(originRow), function(lid) {
        setRowHighlight(rowByLid(lid), false, false, true, false);
    });
}

function clearAllRows() {
    mapQsa(document, "article.post_row", function(row) {
        setRowHighlight(row, false, false, false, false);
    });
}

function forceRestoreAllRows() {
    mapQsa(document, "article.post_row", function(row) {
        stickyRowHighlight(row, false, false, false, false);
        setRowHighlight(row, false, true, true, false);
    });
}

function restoreAllRows() {
    mapQsa(document, "article.post_row", function(row) {
        setRowHighlight(row, false, true, true, false);
    });
}

function getReplies(row) {
    return mapQsa(row, "a.reply_link", function(link) {
        return link.getAttribute("data-reply");
    });
}

function setDefaultRowColors(row) {
    setDefaultColors(row, "a.post_no");
    setDefaultColors(row, "a.reply_target");
    setDefaultColors(row, "div.comment_body");
}

function setDefaultColors(row, query) {
    var e = qs(row, query);
    e.setAttribute("data-default_bgc", e.style.backgroundColor);
    e.setAttribute("data-default_text", e.style.color);
}

function setRowHighlight(row, ph, pn, rt, co) {
    mapRowElements(row, toggleHighlight, ph, pn, rt, co)
}

function stickyRowHighlight(row, ph, pn, rt, co) {
    mapRowElements(row, stickyHighlight, ph, pn, rt, co)
}

function mapRowElements(row, f, ph, pn, rt, co) {
    f(qs(row, "aside.post_header"), ph);
    f(qs(row, "a.post_no"), pn);
    f(qs(row, "a.reply_target"), rt);
    f(qs(row, "div.comment_body"), co);
}

function stickyHighlight(e, val) {
    if (val) {
        e.setAttribute("data-sticky_highlight", "true");
    } else {
        e.setAttribute("data-sticky_highlight", "");
    }
}

function toggleHighlight(e, val) {
    var bgc;
    var textColor;

    if (e.getAttribute("data-sticky_highlight")) {
        return;
    }

    if (typeof val == "object") {
        bgc = altDarkColor(val.getAttribute("data-hi_bgc"));
        textColor = val.getAttribute("data-hi_text");
    } else if (typeof val == "string") {
        bgc = val;
        textColor = "black";
    } else if (val) {
        bgc = e.getAttribute("data-hi_bgc");
        textColor = e.getAttribute("data-hi_text");
    } else {
        bgc = e.getAttribute("data-default_bgc");
        textColor = e.getAttribute("data-default_text");
    }

    e.style.backgroundColor = bgc;
    e.style.color = textColor;
}
    
function colorPostLinks(row) {
    var backRef = qs(row, "a.reply_target");
    var targetId = backRef.getAttribute("data-target");

    if (targetId == 0) {
        return;
    }

    var threadId = row.getAttribute("data-thread_id");
    var thread = getThreadById(threadId);
    var target = constrainedRowByLid(thread, targetId);

    if (!target) {
        return;
    }

    var targetColor = getRainbowColor(target);
    backRef.setAttribute("data-hi_bgc", targetColor);
    backRef.setAttribute("data-hi_text", "black");
    backRef.style.backgroundColor = targetColor;
    backRef.style.color = "black";
}

function getThreadById(id) {
    return qs(document, 'div.thread[data-thread_id="' + id + '"]')
}

function makeColorGenerator() {
    var hsl = {
        hue: 0,
        saturation: 70,
        lightness: 80,
        toString: function() {
            return "hsl(" + this.hue + ", " +
            this.saturation + "%, " +
            this.lightness + "%)";
        },
    }

    return function() {
        hsl.hue = (hsl.hue + 113) % 360;
        hsl.lightness = (hsl.lightness + 89) % 20 + 65;
        return hsl.toString();
    }
}

function getRainbowColor(row) {
    var postNo = qs(row, "a.post_no");
    var bgc = postNo.getAttribute("data-hi_bgc");

    if (!bgc) {
        var bgc = global.nextColor();
        postNo.setAttribute("data-hi_bgc", bgc);
        postNo.style.backgroundColor = bgc;

        var hiText = postNo.getAttribute("data-hi_text");
        if (!hiText) {
            postNo.style.color = "black";
            postNo.setAttribute("data-hi_text", "black");
        }
    }

    return bgc;
}

function altDarkColor(str) {
    return hslMask(str, 0, -40, -5);
}

function altLightColor(str) {
    var hsl = stringToHsl(str);
    return hslToString({
        hue: hsl.hue,
        saturation: hsl.saturation,
        lightness: 90,
    });
}

function hslMask(str, h, s, l) {
    var hsl = stringToHsl(str);
    hsl.hue += h;
    hsl.saturation += s;
    hsl.lightness += l;
    return hslToString(hsl);
}

function stringToHsl(str) {
    var match = /(\d+).*?(\d+).*?(\d+)/.exec(str);
    return {
        hue:        match[1] | 0,
        saturation: match[2] | 0,
        lightness:  match[3] | 0,
    }
}

function hslToString(hsl) {
    return "hsl(" + hsl.hue　+　", " + 
                    hsl.saturation + "%, " +
                    hsl.lightness +　"%)";
}

function getVisibleElem(query) {
    var suffixes = ['1', '4', '6', '7', '9', 'A', 'B', 'D'];
    var selectors = suffixes.map(function(e) { return '[id*="O' +e+ '"]' }); 

    for (var i=0; i < selectors.length; i++) {
        var found = qs(document, query + selectors[i]);
        if (found) return found;
    }
}

function showRowNotifier(row, text, bgColor, textColor) {
    row.style.backgroundColor = bgColor;
    qs(row, "div.comment_body").style.backgroundColor = bgColor;

    var indicator = qs(row, "div.action_indicator");
    indicator.textContent = text;
    indicator.style.visibility = "visible";

    mapQsa(row, "div.header_col, a.report_post, a.fold_posts", function(e) {
        e.style.color = textColor;
    });

    var rowNotifier = qs(row, "aside.image_highlight");
    if (rowNotifier) rowNotifier.style.backgroundColor = bgColor;
}

function xhrOverrideSubmit(e) {
    if (e.target.name !== "add_post") {
        return;
    }

    haltEvent(e);
    xhrPost();
    recoverUserHan();
    mapFormFields(saveAndClear);
    getVisibleElem("input.upload").value = null;
    getVisibleElem("textarea.comment").focus();
}

function xhrPost() {
    var postData = new FormData(qs(document, "form#add_post"));
    postData.append("no_redirect", "true");

    var xhr = new XMLHttpRequest();
    xhr.addEventListener("load", displayPostResponse, false);
    xhr.upload.addEventListener("progress", showXhrProgress, false);
    xhr.upload.addEventListener("load", completeXhrProgress, false);
    xhr.open("POST", "/post", true);
    xhr.send(postData);
}

function displayPostResponse(e) {
    var response = parseResponse(this);
    if (response) { showPostError(response) }
}

function showXhrProgress(e) {
    if (!e.lengthComputable) { return };
    global.submit.value = (((e.loaded / e.total) * 100) | 0) + "%";

    if (!global.constraints.uploading) {
        global.constraints.uploading = true;
        selectSubmitButtonState();
    }
}

function completeXhrProgress(e) {
    global.constraints.uploading = false;
    global.submit.value = "Submit";
    selectSubmitButtonState();
}

function parseResponse(response) {
    if (!response.responseText) {
        return null;
    }

    var parser = new DOMParser()
    var response = parser.parseFromString(response.responseText, "application/xhtml+xml");
    var e = qs(response, "#message_text");
    return e ? e.innerHTML : null;
}

function showPostError(msg) {
    writeToMsgDisplay(msg);
    mapFormFields(restoreOldValue);
}

function writeToMsgDisplay(text) {
    var xhrDisplay = global.xhrDisplay;

    if (xhrDisplay.old === undefined) {
        xhrDisplay.old = xhrDisplay.innerHTML;
    }

    if (text) {
        xhrDisplay.style.color = "red";
        xhrDisplay.innerHTML = text;
    } else {
        xhrDisplay.innerHTML = xhrDisplay.old;
        xhrDisplay.style.color = null;
    }
}

function restoreOldValue(elem) {
    elem.value = elem.oldValue;
}

function saveAndClear(elem) {
    elem.oldValue = elem.value;
    elem.value = null;
}

function mapFormFields(f) {
    var skipNull = function(e) { return e && f(e) };
    skipNull(getVisibleElem("textarea.comment"));
    skipNull(getVisibleElem("input.reply_to"));
}

function openPostingSocket() {
    var threadId = global.tdiv.getAttribute("data-thread_id");
    var ws = new WebSocket("ws://" + location.host + "/ws_post/" + threadId);
    if (ws) {
        ws.addEventListener("message", insertPost, false);
    }
}

function insertPost(e) {
    var stayDown = elementInViewport(qs(document, "footer"));
    global.tdiv.insertAdjacentHTML("beforeEnd", e.data);
    var insertedPost = getLatestPost();
    bindReplyToTarget(insertedPost);
    decorateRow(insertedPost);
    announcePost(insertedPost);
    recoverUserHan();

    if (hiddenByFold(insertedPost)) {
        insertedPost.style.display = "none";
    }

    if (stayDown) {
        getVisibleElem("input.reply_to").scrollIntoView();
    }
}

function getLatestPost() {
    var rows = qsa(document, "div.thread > article.post_row");
    return rows[rows.length - 1];
}

function announcePost(row) {
    var postNo = row.getAttribute("data-post_lid");
    var announcePost = new CustomEvent("insertedPost", {"detail":{"id": postNo}});
    document.dispatchEvent(announcePost);
}

function bindReplyToTarget(row) {
    var targetId = qs(row, "a.reply_target").textContent.trim();

    if (!targetId) {
        return;
    }

    var postNo = row.getAttribute("data-post_lid");
    annotateReplyTarget(rowByLid(targetId), postNo);
}

function hiddenByFold(row) {
    var postNo = row.getAttribute("data-post_lid");
    var activeFold = qs(document, "article.post_row[data-pivot='true']")
    var targetId = qs(row, "a.reply_target").textContent.trim();
    var badTarget = (!targetId) || rowByLid(targetId).style.display == "none";
    return activeFold && badTarget
}

function annotateReplyTarget(targetRow, responseId) {
    var threadId = global.tdiv.getAttribute('data-thread_id');
    var link = formatLink(threadId, responseId);
    qs(targetRow, "div.comment_body").insertAdjacentHTML("beforeEnd", link);
}

function formatLink(tid, lid) {
    return '<a href="/t/' + tid + '#' + lid +
           '" class="reply_link" data-reply="' + lid +
           '">→' + lid + '</a>';
}

function elementInViewport(e) {
    var rect = e.getBoundingClientRect();
    var marginOfError = 200;

    return (
        rect.top >= 0 &&
        rect.left >= 0 &&
        rect.bottom <= (window.innerHeight + marginOfError) &&
        rect.right <= (window.innerWidth + marginOfError)
    );
}

function loadAutoComplete() {
    var xhr = new XMLHttpRequest();
    xhr.onload = populateSearchCandidates;
    xhr.open("GET", "/tags_autocomplete", true);
    xhr.send();
}

function populateSearchCandidates() {
    var tagList = this.responseText.trim().split("\n")
    var makeOption = function(line) { return "<option>" +line+ "</option>" };
    var list = qs(document, "datalist#search_list");
    list.innerHTML = tagList.map(makeOption);
}

function recoverUserHan() {
    var ident = getIdent();
    var query = 'aside.post_header[data-ident="' + ident + '"]';
    var related = qs(document, query);

    if (related) {
        userHanFromRelated(related);
    }
}

function userHanFromRelated(related) {
    global.thisHan.style.color = hslToString(hslFromAttributes(related));
    global.thisHan.textContent = related.getAttribute("data-char");
    global.thisHan.style.visibility = "visible";
}

function getThreadKey() {
    var threadId = global.tdiv.getAttribute("data-thread_id");
    var threadMarker = global.tdiv.getAttribute("data-random_mark");
    return "thread_" + threadId + "_" + threadMarker;
}

function getIdent() {
    var key = getThreadKey();
    var storedId = localStorage.getItem(key);
    if (storedId) {
        return storedId;
    }

    var last = localStorage.getItem("op_ident");
    if (last) {
        localStorage.setItem(key, last);
        return last;
    }

    var newKey = generateUserNum();
    localStorage.setItem(key, newKey);
    return newKey;
}

function generateUserNum() {
    return 1 + Math.floor(Math.random() * 999999999999999);
}

function submitOpUserNum() {
    setUserNum("op_ident", generateUserNum());
}

function setUserNum(key, num) {
    qs(document, "input#user_num").value = num;
    localStorage.setItem(key, num);
}

function hslFromAttributes(e) {
    return {
        hue: e.getAttribute("data-hue"),
        saturation: e.getAttribute("data-saturation"),
        lightness: e.getAttribute("data-lightness")
    }
}

function checkCommentLength(constraints) {
    var constraints = global.constraints;
    var max = constraints.getAttribute("data-comment_length");
    var length = global.comment.value.length;
    var msg = "Comment too long. (" + length + "/" + max + ")";
    constraints.commentLength = length > max;
    testConstraint(constraints.commentLength, msg)
}

function checkFileSize() {
    if (!global.upload.files.length) {
        return;
    }

    var file = global.upload.files[0];
    var mediaType = getMediaType(file.type);
    var max = global.constraints.getAttribute("data-" + mediaType + "_size");
    var msg = "File too large. (" + bSize(file.size) + "/" + bSize(max) + ")";
    global.constraints.fileSize = file.size > max;
    testConstraint(global.constraints.fileSize, msg);
}

function checkFileType() {
    if (!global.upload.files.length) {
        return;
    }

    var fileType = global.upload.files[0].type;
    var mediaType = getMediaType(fileType);
    var msg = "Invalid file type: " + fileType;
    global.constraints.fileType = mediaType == "Unknown";
    testConstraint(global.constraints.fileType, msg);
}

function getMediaType(fmt) {
    return testMediaCategory("image", "data-image_formats", fmt) ||
           testMediaCategory("video", "data-video_formats", fmt) ||
           testMediaCategory("audio", "data-audio_formats", fmt) ||
           "Unknown";
}

function testMediaCategory(category, selector, fmt) {
    var fmts = parseSlice(global.constraints.getAttribute(selector));
    return (fmts.indexOf(fmt) != -1) ? category : null;
}

function parseSlice(s) {
    return s.substr(1, s.length-2).split(" ");
}

function bSize(bytes) {
   if(bytes == 0) return '0 Byte';
   var k = 1000;
   var sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
   var i = Math.floor(Math.log(bytes) / Math.log(k));
   return (bytes / Math.pow(k, i)).toPrecision(3) + ' ' + sizes[i];
}

function testConstraint(truth, msg) {
    if (truth) {
        writeToMsgDisplay(msg);
        selectSubmitButtonState();
    }
}

function selectSubmitButtonState() {
    var constraints = global.constraints;
    global.submit.disabled = constraints.commentLength || 
                             constraints.fileSize      ||
                             constraints.fileType      ||
                             constraints.uploading;
}


function addCommentHandlers() {
    global.comment.addEventListener("input", checkCommentLength, false);
    global.upload.addEventListener("change", checkFileSize, false);
    global.upload.addEventListener("change", checkFileType, false);
}

function autoPopulateTags() {
    var tags = qs(document, "body#query").getAttribute("data-query").split(" ");
    getVisibleElem("input.tag_entry").value = filterTags(tags);
}

function filterTags(list) {
    var noQualifiers = function(e) { return e[0] != '-' && e[0] != '+' };
    return list.filter(noQualifiers).join(" ");
}

function colorSummaryThreads() {
    mapQsa(document, "div.thread", function(thread) {
        mapQsa(thread, "article.post_row", decorateRow);
    });
}

function getQueryString() {
  var result = {};
  var queryString = location.search.slice(1);
  var re = /([^&=]+)=([^&]*)/g;
  var m;

  while (m = re.exec(queryString)) {
    result[decodeURIComponent(m[1])] = decodeURIComponent(m[2]);
  }

  return result;
}

function queryInit() {
    window.addEventListener("submit", submitOpUserNum, false);
    autoPopulateTags();

    if (global.viewMode == "summary") {
        colorSummaryThreads();
    }
}

function threadInit() {
    setUserNum(getThreadKey(), getIdent());
    window.addEventListener("submit", xhrOverrideSubmit, false);
    mapQsa(document, "article.post_row", decorateRow);
    openPostingSocket();

    var pivot = getQueryString()["pivot"];
    if (pivot) {
        foldConvo(rowByLid(pivot));
    }
}

function main() {
    defineGlobals();

    if (global.pageType == "query") {
        queryInit();
    }

    if (global.pageType == "thread") {
        threadInit();
    }

    addCommentHandlers();
    qsael(document, "input#tag_search", "focus", loadAutoComplete, false);
}

window.addEventListener("load", main, false);   

