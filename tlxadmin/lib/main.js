var pageMod = require("sdk/page-mod");
var self = require("sdk/self");
 
pageMod.PageMod({
    include: /.*meido:7842.*/,
    contentScriptFile: self.data.url("amend.js")
});
