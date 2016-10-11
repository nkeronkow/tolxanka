"use strict";

function TLXA_NAMESPACE() {
//-----------------------BEGIN---------------------

function qsa(elem, query) { return elem.querySelectorAll(query); }
function qs(elem, query) { return elem.querySelector(query); }
function getThreadId() { return qs(content.document, "div.thread").getAttribute("data-thread_id"); }

function highlightRow(row, text, bgColor, textColor) {
    if (row.sustainRowHighlight) return;
    var commentBody = qs(row, 'div.comment_body');
    var colorTextElems = qsa(row, 'div.header_col, a.report_post, a.fold_posts');
    var imageHighlighter = qs(row, 'aside.image_highlight');
    var indicator = qs(row, 'div.action_indicator');

    row.style.backgroundColor = bgColor;
    commentBody.style.backgroundColor = bgColor;
    indicator.textContent = text;
    indicator.style.visibility = "visible";

    for (var i=0; i < colorTextElems.length; i++) {
        colorTextElems[i].style.color = textColor;
    }

    if (imageHighlighter) imageHighlighter.style.backgroundColor = bgColor;
}

function amendFooter(modPostOption) {
    var footerRight = qs(content.document, "div#footer_right");
    var adminSection = '<form name="admin_section" id="admin_section" method="post" action="/admin_mod_posts_landing">';

    if (adminRights.ViewRestrictedTags) {
        adminSection += '<a href="/cat/!%3F_admin">!?_admin</a> ';
    }

    if (modPostOption && adminRights.DeletePost && adminRights.BanUser) {
        adminSection += '<button form="admin_section" id="mod_posts" type="submit">Mod Posts</button>' +
                          '<a href="#" id="select_all_posts">Select All</a>';
    }
    adminSection += '</form>';

    footerRight.insertAdjacentHTML('beforeend', adminSection);
    var lockedMsg = qs(content.document, "h3.locked_msg");
    if (lockedMsg) {
        lockedMsg.style.display = "none";
        qs(content.document, "div#footer_left").style.display = "inline";
        qs(content.document, "div#footer_right").style.display = "flex";
    }

    if (modPostOption && adminRights.DeletePost && adminRights.BanUser) {
        var selectAll = qs(content.document, "a#select_all_posts");
        selectAll.addEventListener("click", selectAllPosts, true);
    }
}

function amendPostTable() {
    var threadId = getThreadId();
    var lockedOption;
    var isLocked = qs(content.document, "div.thread").getAttribute("data-locked");

    var modOptions = '<div id="option_cell">'

    if (adminRights.PostWithRole) {
        modOptions += '<label class="mod_option"><input name="post_with_role" type="checkbox" form="add_post" /> Post with staff role</label>';
    }

    if (adminRights.StickyThread) {
        modOptions += '<a href="/admin_sticky_thread_landing/' +threadId+ '" class="mod_option">Sticky thread</a>';
    }

    if (adminRights.LockThread) {
        if (isLocked == "true") {
            modOptions += '<a href="/admin_lock_thread/' +threadId+   '/false" class="mod_option">Unlock thread</a>';
        } else {
            modOptions += '<a href="/admin_lock_thread/' +threadId+   '/true"  class="mod_option">Lock thread</a>';
        }
    }

    if (adminRights.DeleteThread) {
        '<a href="/admin_delete_thread/' +threadId+ '" class="mod_option">Delete thread</a>';
    }

    modOptions += '</div>';

    var footerRight = qs(content.document, "div#footer_right");
    footerRight.insertAdjacentHTML('afterbegin', modOptions);
}

function amendPostRow(row) {
    var threadId = row.getAttribute("data-thread_id");
    var postGid = row.getAttribute("data-post_gid");
    var ai = qs(row, 'div.action_indicator');

    var getUserHtml = '<a href="/posts_by_user/' + postGid + '" class="side_control thin get_user">?</a>';
    ai.insertAdjacentHTML('afterEnd', getUserHtml);
    var getUser = qs(row, 'a.get_user');
    installActionHighlighters(getUser, "USER", "#bedc90");

    var modPostHtml =
        '<div class="side_control thin mod_post">' +
        '<input type="checkbox" name="selected_post" class="selected_post" form="admin_section" value="' + postGid + '" />' +
        '</div>';

    ai.insertAdjacentHTML('afterEnd', modPostHtml);
    var modPost = qs(row, 'div.mod_post');
    installActionHighlighters(modPost, "SELECT", "#dcba90");

    var reportPost = qs(row, 'a.report_post');
    reportPost.classList.add('thin');
}

function installActionHighlighters(elem, text, color) {
    elem.addEventListener("mouseover", function(e) { highlightRow(this.parentNode, text, color, "black") }, true);
    elem.addEventListener("mouseout", function(e) { highlightRow(this.parentNode, null, null, null) }, true);
}

function amendInsertedPostRow(e) {
    var id = e.detail.id;
    var row = qs(content.document, 'article.post_row[data-post_lid="' + id + '"]');
    amendPostRow(row);
}

function modThreadPage() {
    if (adminRights.DeletePost && adminRights.BanUser) {
        content.document.addEventListener("insertedPost", amendInsertedPostRow, false); 
        var rows = qsa(content.document, 'div.thread > article.post_row');
        for (var i=0; i < rows.length; i++) amendPostRow(rows[i]);
    }
    amendPostTable();
    amendFooter(true);
}

function modQueryPage() {
    amendFooter(false);
}

function selectAllPosts() {
    var boxes = qsa(content.document, 'input.selected_post');
    for (var i=0; i < boxes.length; i++) {
        boxes[i].checked = "on";
    }
}

function tlxInit() {
    switch (document.body.getAttribute('id')) {
        case "thread": modThreadPage(); break;
        case "query":  modQueryPage(); break;
    }
}

function log(msg) {
    console.log("tlxadmin: " + msg)
}

tlxInit();

//-----------------------END-----------------------
} TLXA_NAMESPACE();


